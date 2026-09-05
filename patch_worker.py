import re

with open("internal/execution/worker.go", "r") as f:
    content = f.read()

# We want to replace the `for _, id := range order {` loop
old_loop = """	for _, id := range order {
		if err = ctx.Err(); err != nil {
			return nil, true, fmt.Errorf("execution interrupted")
		}
		n := nodes[id]
		m := catalog[n.Plugin+"@"+n.Version]
		cached, cacheErr := w.Store.CachedStep(ctx, r.ID, id)
		if cacheErr == nil && m.Kind != "resource" {
			outputs[id] = cached
			continue
		}
		if cacheErr != nil && !platform.IsMissing(cacheErr) {
			return nil, false, fmt.Errorf("cannot load step checkpoint")
		}
		inputs := map[string]any{}
		if m.Kind == "trigger" {
			inputs["event"] = triggerInput
		}
		for _, e := range revision.Graph.Edges {
			if e.Target == id {
				source, ok := outputs[e.Source].(map[string]any)
				if !ok {
					return nil, false, fmt.Errorf("missing dependency")
				}
				v, exists := source[e.SourcePort]
				if !exists {
					return nil, false, fmt.Errorf("dependency did not produce %s", e.SourcePort)
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
		for _, p := range m.Inputs {
			if v, ok := inputs[p.Name]; ok {
				values := []any{v}
				if p.Multiple {
					values = v.([]any)
				}
				for _, value := range values {
					if err = platform.ValidateSchema(p.Schema, value); err != nil {
						return nil, false, fmt.Errorf("node %s input %s failed validation", id, p.Name)
					}
				}
			}
		}
		if err = w.Store.StartStep(ctx, r, id); err != nil {
			return nil, false, err
		}
		result, callErr := w.invoke(ctx, r, revision.Name, n, m, inputs)
		if callErr != nil {
			if err = w.Store.EndStep(ctx, r, id, "failed", nil, "worker communication failed"); err != nil {
				return nil, false, err
			}
			return nil, true, fmt.Errorf("node %s worker communication failed", id)
		}
		if result.Error != nil {
			if err = w.Store.EndStep(ctx, r, id, "failed", nil, result.Error.Code); err != nil {
				return nil, false, err
			}
			return nil, result.Error.Retryable, fmt.Errorf("node %s: %s", id, result.Error.Code)
		}
		for _, p := range m.Outputs {
			v, ok := result.Outputs[p.Name]
			if !ok {
				if p.Required {
					return nil, false, fmt.Errorf("node %s missing output %s", id, p.Name)
				}
				continue
			}
			if err = platform.ValidateSchema(p.Schema, v); err != nil {
				return nil, false, fmt.Errorf("node %s invalid output %s", id, p.Name)
			}
			if p.Kind == "resource" {
				descriptor, ok := v.(map[string]any)
				if !ok || descriptor["protocol"] != "workflow.resource/v1" || descriptor["type"] != p.ResourceType || descriptor["plugin"] != m.Name || descriptor["version"] != m.Version || descriptor["digest"] != m.Digest {
					return nil, false, fmt.Errorf("node %s returned an invalid resource descriptor", id)
				}
			}
		}
		declared := map[string]bool{}
		for _, p := range m.Outputs {
			declared[p.Name] = true
		}
		for key := range result.Outputs {
			if !declared[key] {
				return nil, false, fmt.Errorf("node %s returned undeclared output", id)
			}
		}
		if err = w.Store.EndStep(ctx, r, id, "succeeded", result.Outputs, ""); err != nil {
			return nil, false, err
		}
		outputs[id] = result.Outputs
	}"""

new_loop = """	var outputsMu sync.Mutex
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
						return fmt.Errorf("missing dependency")
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
			result, callErr := w.invoke(egCtx, r, revision.Name, n, m, inputs)
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
	}"""

content = content.replace(old_loop, new_loop)
with open("internal/execution/worker.go", "w") as f:
    f.write(content)
