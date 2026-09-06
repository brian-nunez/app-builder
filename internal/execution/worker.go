package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brian-nunez/app-builder/internal/platform"
	"github.com/brian-nunez/app-builder/sdk/go/plugin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Worker struct {
	Store           *platform.Store
	Endpoint, Token string
	Client          *http.Client
	// Capacity is the number of concurrent invocations the plugin worker accepts.
	// It is read from the worker's own /catalog report so the limit has one owner.
	Capacity int

	slots chan struct{}
	runs  chan struct{}
}

// Start prepares the scheduler's admission limits from the worker's capacity.
func (w *Worker) Start() {
	if w.Capacity < 1 {
		w.Capacity = 1
	}
	w.slots = make(chan struct{}, w.Capacity)
	w.runs = make(chan struct{}, w.Capacity)
}

func (w *Worker) Run(ctx context.Context) error {
	if w.slots == nil {
		w.Start()
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Claim only what this replica has room to run. Pulling the whole queue
			// into goroutines that then queue again downstream is not backpressure.
			for w.reserve() {
				r, err := w.Store.Claim(ctx)
				if err != nil {
					w.release()
					if !platform.IsMissing(err) {
						slog.ErrorContext(ctx, "claim failed", "error", err)
					}
					break
				}
				wg.Add(1)
				go func(run platform.Run) {
					defer wg.Done()
					defer w.release()
					w.execute(ctx, run)
				}(r)
			}
		}
	}
}

func (w *Worker) reserve() bool {
	select {
	case w.runs <- struct{}{}:
		return true
	default:
		return false
	}
}

func (w *Worker) release() { <-w.runs }
func (w *Worker) execute(parent context.Context, r platform.Run) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": r.TraceParent})
	ctx, span := otel.Tracer("workflow.engine").Start(ctx, "workflow.execute", trace.WithAttributes(attribute.String("workflow.id", r.WorkflowID), attribute.String("workflow.run.id", r.ID), attribute.Int("workflow.revision", r.Revision)))
	defer span.End()
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				if err := w.Store.Heartbeat(ctx, r); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	output, retry, err := w.graph(ctx, r)
	status := "succeeded"
	message := ""
	if err != nil {
		status = "failed"
		message = err.Error()
		span.SetStatus(codes.Error, message)
		slog.ErrorContext(ctx, "workflow failed", "workflow.run.id", r.ID, "error", message)
	}
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if err = w.Store.Complete(finishCtx, r, status, output, message, retry); err != nil {
		slog.ErrorContext(ctx, "run completion failed", "workflow.run.id", r.ID, "error", err)
	}
}
func (w *Worker) graph(ctx context.Context, r platform.Run) (map[string]any, bool, error) {
	if r.Attempt > 3 {
		return nil, false, fmt.Errorf("execution recovery limit exceeded")
	}
	revision, err := w.Store.Revision(ctx, r.WorkflowID, r.Revision)
	if err != nil {
		return nil, false, err
	}
	catalog, err := w.Store.Catalog(ctx)
	if err != nil {
		return nil, false, err
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("workflow.name", revision.Name))
	order, err := platform.ValidateGraph(revision.Graph, catalog, true)
	if err != nil {
		return nil, false, err
	}
	var triggerInput map[string]any
	if err = w.Store.Cipher.Open(r.Input, r.WorkflowID, &triggerInput); err != nil {
		return nil, false, fmt.Errorf("cannot decrypt run input")
	}
	outputs := map[string]any{}
	nodes := map[string]plugin.Node{}
	for _, n := range revision.Graph.Nodes {
		nodes[n.ID] = n
	}
	var outputsMu sync.Mutex
	doneChans := map[string]chan struct{}{}
	for _, id := range order {
		doneChans[id] = make(chan struct{})
	}
	preds := map[string][]string{}
	for _, e := range revision.Graph.Edges {
		preds[e.Target] = append(preds[e.Target], e.Source)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	var isRetryable bool
	var errMu sync.Mutex
	setRetryable := func(retry bool) {
		errMu.Lock()
		if retry {
			isRetryable = true
		}
		errMu.Unlock()
	}

	for _, id := range order {
		id := id // capture loop variable
		eg.Go(func() error {
			defer close(doneChans[id])
			// Wait for predecessors
			for _, p := range preds[id] {
				select {
				case <-doneChans[p]:
				case <-egCtx.Done():
					setRetryable(true)
					return fmt.Errorf("execution interrupted")
				}
			}
			// Check if context is already done
			if err := egCtx.Err(); err != nil {
				setRetryable(true)
				return fmt.Errorf("execution interrupted")
			}

			n := nodes[id]
			m := catalog[n.Plugin+"@"+n.Version]
			cached, cacheErr := w.Store.CachedStep(egCtx, r.ID, id)
			if cacheErr == nil && m.Kind != "resource" {
				outputsMu.Lock()
				outputs[id] = cached
				outputsMu.Unlock()
				return nil
			}
			if cacheErr != nil && !platform.IsMissing(cacheErr) {
				return fmt.Errorf("cannot load step checkpoint")
			}

			inputs := map[string]any{}
			if m.Kind == "trigger" {
				inputs["event"] = triggerInput
			}

			outputsMu.Lock()
			for _, e := range revision.Graph.Edges {
				if e.Target == id {
					source, ok := outputs[e.Source].(map[string]any)
					if !ok {
						outputsMu.Unlock()
						// Predecessor must have failed. Exit cleanly so errgroup retains original error.
						return nil
					}
					v, exists := source[e.SourcePort]
					if !exists {
						outputsMu.Unlock()
						return fmt.Errorf("dependency did not produce %s", e.SourcePort)
					}
					multiple := false
					for _, p := range m.Inputs {
						if p.Name == e.TargetPort {
							multiple = p.Multiple
						}
					}
					if multiple {
						values, _ := inputs[e.TargetPort].([]any)
						inputs[e.TargetPort] = append(values, v)
					} else {
						inputs[e.TargetPort] = v
					}
				}
			}
			outputsMu.Unlock()

			for _, p := range m.Inputs {
				if v, ok := inputs[p.Name]; ok {
					values := []any{v}
					if p.Multiple {
						values = v.([]any)
					}
					for _, value := range values {
						if err := platform.ValidateSchema(p.Schema, value); err != nil {
							return fmt.Errorf("node %s input %s failed validation", id, p.Name)
						}
					}
				}
			}

			if err := w.Store.StartStep(egCtx, r, id); err != nil {
				return err
			}

			result, callErr := w.call(egCtx, r, revision.Name, n, m, inputs)

			if callErr != nil {
				if err := w.Store.EndStep(egCtx, r, id, "failed", nil, "worker communication failed"); err != nil {
					return err
				}
				setRetryable(true)
				return fmt.Errorf("node %s worker communication failed", id)
			}
			if result.Error != nil {
				if err := w.Store.EndStep(egCtx, r, id, "failed", nil, result.Error.Code); err != nil {
					return err
				}
				setRetryable(result.Error.Retryable)
				return fmt.Errorf("node %s: %s", id, result.Error.Code)
			}
			for _, p := range m.Outputs {
				v, ok := result.Outputs[p.Name]
				if !ok {
					if p.Required {
						return fmt.Errorf("node %s missing output %s", id, p.Name)
					}
					continue
				}
				if err := platform.ValidateSchema(p.Schema, v); err != nil {
					return fmt.Errorf("node %s invalid output %s", id, p.Name)
				}
				if p.Kind == "resource" {
					descriptor, ok := v.(map[string]any)
					if !ok || descriptor["protocol"] != "workflow.resource/v1" || descriptor["type"] != p.ResourceType || descriptor["plugin"] != m.Name || descriptor["version"] != m.Version || descriptor["digest"] != m.Digest {
						return fmt.Errorf("node %s returned an invalid resource descriptor", id)
					}
				}
			}
			declared := map[string]bool{}
			for _, p := range m.Outputs {
				declared[p.Name] = true
			}
			for key := range result.Outputs {
				if !declared[key] {
					return fmt.Errorf("node %s returned undeclared output", id)
				}
			}
			if err := w.Store.EndStep(egCtx, r, id, "succeeded", result.Outputs, ""); err != nil {
				return err
			}

			outputsMu.Lock()
			outputs[id] = result.Outputs
			outputsMu.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, isRetryable, err
	}
	return outputs, false, nil
}

// busy reports that the plugin worker is at capacity and asked the caller to wait.
type busy struct{ after time.Duration }

func (b busy) Error() string { return "plugin worker at capacity" }

func retryAfter(header string) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 && seconds <= 60 {
		return time.Duration(seconds) * time.Second
	}
	return time.Second
}

// call runs one node against the plugin worker, holding a slot from the shared
// pool for the duration and waiting out backpressure instead of failing on it.
func (w *Worker) call(ctx context.Context, r platform.Run, name string, n plugin.Node, m plugin.Manifest, inputs map[string]any) (plugin.Response, error) {
	for attempt := 0; ; attempt++ {
		select {
		case w.slots <- struct{}{}:
		case <-ctx.Done():
			return plugin.Response{}, ctx.Err()
		}
		result, err := w.invoke(ctx, r, name, n, m, inputs)
		<-w.slots
		var saturated busy
		if !errors.As(err, &saturated) {
			return result, err
		}
		if attempt >= maxBusyRetries {
			return plugin.Response{}, fmt.Errorf("plugin worker stayed at capacity")
		}
		select {
		case <-ctx.Done():
			return plugin.Response{}, ctx.Err()
		case <-time.After(saturated.after):
		}
	}
}

const maxBusyRetries = 60

func (w *Worker) invoke(ctx context.Context, r platform.Run, name string, n plugin.Node, m plugin.Manifest, inputs map[string]any) (plugin.Response, error) {
	attrs := []attribute.KeyValue{attribute.String("workflow.name", name), attribute.String("plugin.name", m.Name), attribute.String("plugin.version", m.Version)}
	ctx, span := otel.Tracer("workflow.plugins").Start(ctx, "plugin.execute", trace.WithAttributes(append(attrs, attribute.String("workflow.run.id", r.ID), attribute.String("workflow.node.id", n.ID), attribute.String("workflow.node.name", n.Name), attribute.Int("workflow.attempt", r.Attempt))...))
	defer span.End()
	start := time.Now()
	meter := otel.Meter("workflow.plugins")
	duration, _ := meter.Float64Histogram("workflow.plugin.duration", metric.WithUnit("s"))
	calls, _ := meter.Int64Counter("workflow.plugin.invocations")
	defer func() {
		duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
		calls.Add(ctx, 1, metric.WithAttributes(attrs...))
	}()
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	request := plugin.Request{Protocol: plugin.Protocol, Plugin: m.Name, Version: m.Version, Digest: m.Digest, Config: n.Config, Inputs: inputs, LogFields: n.LogFields, Context: plugin.ExecutionContext{WorkflowID: r.WorkflowID, WorkflowName: name, Revision: r.Revision, RunID: r.ID, NodeID: n.ID, NodeName: n.Name, Attempt: r.Attempt, IdempotencyKey: r.ID + ":" + n.ID, TraceParent: carrier["traceparent"], TraceState: carrier["tracestate"]}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.Endpoint+"/execute", bytes.NewReader(platform.JSON(request)))
	if err != nil {
		return plugin.Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+w.Token)
	req.Header.Set("Content-Type", "application/json")
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	response, err := w.Client.Do(req)
	if err != nil {
		return plugin.Response{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusServiceUnavailable {
		// The worker is saturated. That is backpressure, not a failure: wait for
		// the interval it asked for and let the caller try this node again.
		return plugin.Response{}, busy{after: retryAfter(response.Header.Get("Retry-After"))}
	}
	if response.StatusCode != 200 {
		return plugin.Response{}, fmt.Errorf("worker status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
	if err != nil {
		return plugin.Response{}, err
	}
	if len(data) > 8<<20 {
		return plugin.Response{}, fmt.Errorf("worker response too large")
	}
	var result plugin.Response
	if err = json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.Protocol != plugin.Protocol {
		return result, fmt.Errorf("worker protocol mismatch")
	}
	if result.Error != nil {
		span.SetStatus(codes.Error, result.Error.Code)
		failures, _ := meter.Int64Counter("workflow.plugin.failures")
		failures.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	return result, nil
}
