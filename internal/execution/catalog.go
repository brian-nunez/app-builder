package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/brian-nunez/app-builder/sdk/go/plugin"
)

// Report is what the plugin worker says about itself: how many invocations it
// will run at once, and exactly which artifacts it can execute.
type Report struct {
	Protocol string `json:"protocol"`
	Capacity int    `json:"capacity"`
	Plugins  []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Digest  string `json:"digest"`
	} `json:"plugins"`
}

// Catalog asks the worker what it has installed, retrying while it starts up.
func (w *Worker) Catalog(ctx context.Context) (Report, error) {
	var last error
	for attempt := range 10 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Report{}, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		report, err := w.catalog(ctx)
		if err == nil {
			return report, nil
		}
		last = err
	}
	return Report{}, last
}

func (w *Worker) catalog(ctx context.Context) (Report, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, w.Endpoint+"/catalog", nil)
	if err != nil {
		return Report{}, err
	}
	request.Header.Set("Authorization", "Bearer "+w.Token)
	response, err := w.Client.Do(request)
	if err != nil {
		return Report{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return Report{}, err
	}
	if response.StatusCode != http.StatusOK {
		return Report{}, fmt.Errorf("plugin worker catalog returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var report Report
	if err = json.Unmarshal(body, &report); err != nil {
		return Report{}, err
	}
	if report.Protocol != plugin.Protocol {
		return Report{}, fmt.Errorf("plugin worker speaks %q, platform speaks %q", report.Protocol, plugin.Protocol)
	}
	if report.Capacity < 1 {
		return Report{}, fmt.Errorf("plugin worker reported capacity %d", report.Capacity)
	}
	return report, nil
}

// Agree confirms the platform and the worker resolved the same plugin artifacts.
//
// The two run from separate filesystems. Without this check a workflow can be
// built against a plugin the worker cannot execute, and the mismatch only shows
// up as a failed node long after the deployment that caused it.
func Agree(installed []plugin.Package, report Report) error {
	platform := map[string]string{}
	for _, item := range installed {
		platform[item.Manifest.Key()] = item.Manifest.Digest
	}
	worker := map[string]string{}
	for _, item := range report.Plugins {
		worker[item.Name+"@"+item.Version] = item.Digest
	}
	var problems []string
	for key, digest := range platform {
		switch other, present := worker[key]; {
		case !present:
			problems = append(problems, key+" is installed on the platform but not on the worker")
		case other != digest:
			problems = append(problems, fmt.Sprintf("%s differs: platform %.12s…, worker %.12s…", key, digest, other))
		}
	}
	for key := range worker {
		if _, present := platform[key]; !present {
			problems = append(problems, key+" is installed on the worker but not on the platform")
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("platform and plugin worker disagree about installed plugins:\n  %s", strings.Join(problems, "\n  "))
}
