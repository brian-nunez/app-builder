package platform

import (
	"testing"

	"github.com/brian-nunez/app-builder/sdk/go/plugin"
)

func TestValidateGraphSuppliesTriggerEventFromRunInput(t *testing.T) {
	manifest := plugin.Manifest{
		Name:         "community.webhook-trigger",
		Version:      "1.0.0",
		Kind:         "trigger",
		ConfigSchema: plugin.Schema{"type": "object"},
		Inputs: []plugin.Port{{
			Name:     "event",
			Title:    "Request",
			Kind:     "data",
			Required: true,
			Schema:   plugin.Schema{"type": "object"},
		}},
	}
	graph := plugin.Graph{Nodes: []plugin.Node{{
		ID:      "trigger",
		Plugin:  manifest.Name,
		Version: manifest.Version,
		Name:    "Inbound webhook",
		Config:  map[string]any{},
	}}}

	if _, err := ValidateGraph(graph, map[string]plugin.Manifest{manifest.Name + "@" + manifest.Version: manifest}, true); err != nil {
		t.Fatalf("trigger event should be supplied by the run input: %v", err)
	}
}
