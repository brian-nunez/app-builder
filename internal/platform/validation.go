package platform

import (
	"encoding/json"
	"fmt"
	"github.com/brian-nunez/app-builder/sdk/go/plugin"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"strings"
)

func ValidateSchema(schema map[string]any, value any) error {
	c := jsonschema.NewCompiler()
	if err := c.AddResource("urn:workflow:schema", schema); err != nil {
		return err
	}
	s, err := c.Compile("urn:workflow:schema")
	if err != nil {
		return err
	}
	return s.Validate(value)
}
func ValidateGraph(g plugin.Graph, catalog map[string]plugin.Manifest, complete bool) ([]string, error) {
	if len(g.Nodes) > 200 || len(g.Edges) > 1000 {
		return nil, fmt.Errorf("workflow exceeds graph limits")
	}
	triggerCount := 0
	responseCount := 0
	nodes := map[string]plugin.Node{}
	degree := map[string]int{}
	adj := map[string][]string{}
	bound := map[string]bool{}
	edgeIDs := map[string]bool{}
	connections := map[string]bool{}
	for _, n := range g.Nodes {
		if n.ID == "" || len(n.ID) > 100 || nodes[n.ID].ID != "" {
			return nil, fmt.Errorf("duplicate or invalid node ID")
		}
		m, ok := catalog[n.Plugin+"@"+n.Version]
		if !ok {
			return nil, fmt.Errorf("plugin %s@%s is unavailable", n.Plugin, n.Version)
		}
		if m.Kind == "trigger" {
			triggerCount++
			if triggerCount > 1 {
				return nil, fmt.Errorf("one trigger per workflow is supported; use separate workflows for independent triggers")
			}
		}
		if err := ValidateSchema(m.ConfigSchema, n.Config); err != nil {
			return nil, fmt.Errorf("%s configuration: %w", n.Name, err)
		}
		allowed := map[string]bool{}
		for _, f := range m.LogFields {
			allowed[f] = true
		}
		for _, f := range n.LogFields {
			if !allowed[f] {
				return nil, fmt.Errorf("undeclared diagnostic field %s", f)
			}
		}
		if n.ResponsePort != "" {
			responseCount++
			valid := false
			for _, port := range m.Outputs {
				if port.Name == n.ResponsePort && port.Kind == "data" {
					valid = true
				}
			}
			if !valid {
				return nil, fmt.Errorf("%s response must reference a declared data output", n.Name)
			}
			if responseCount > 1 {
				return nil, fmt.Errorf("select only one workflow response output")
			}
		}
		nodes[n.ID] = n
		degree[n.ID] = 0
	}
	for _, e := range g.Edges {
		s, ok := nodes[e.Source]
		if !ok {
			return nil, fmt.Errorf("unknown source")
		}
		t, ok := nodes[e.Target]
		if !ok {
			return nil, fmt.Errorf("unknown target")
		}
		if e.ID == "" || edgeIDs[e.ID] {
			return nil, fmt.Errorf("duplicate edge ID")
		}
		edgeIDs[e.ID] = true
		tuple := e.Source + ":" + e.SourcePort + ":" + e.Target + ":" + e.TargetPort
		if connections[tuple] {
			return nil, fmt.Errorf("connection already exists")
		}
		connections[tuple] = true
		var out, in *plugin.Port
		for _, p := range catalog[s.Plugin+"@"+s.Version].Outputs {
			if p.Name == e.SourcePort {
				v := p
				out = &v
			}
		}
		for _, p := range catalog[t.Plugin+"@"+t.Version].Inputs {
			if p.Name == e.TargetPort {
				v := p
				in = &v
			}
		}
		if out == nil || in == nil {
			return nil, fmt.Errorf("unknown port")
		}
		if out.Kind != in.Kind || out.ResourceType != in.ResourceType {
			return nil, fmt.Errorf("incompatible connection types")
		}
		if out.Sensitive && !in.Sensitive {
			return nil, fmt.Errorf("sensitive output requires a sensitive input")
		}
		if ot, ok := out.Schema["type"].(string); ok {
			if it, ok := in.Schema["type"].(string); ok && ot != it {
				return nil, fmt.Errorf("incompatible data types")
			}
		}
		key := e.Target + ":" + e.TargetPort
		if bound[key] && !in.Multiple {
			return nil, fmt.Errorf("input already connected")
		}
		bound[key] = true
		degree[e.Target]++
		adj[e.Source] = append(adj[e.Source], e.Target)
	}
	if complete {
		for _, n := range g.Nodes {
			manifest := catalog[n.Plugin+"@"+n.Version]
			for _, p := range manifest.Inputs {
				if manifest.Kind == "trigger" && p.Name == "event" {
					continue
				}
				if p.Required && !bound[n.ID+":"+p.Name] {
					return nil, fmt.Errorf("%s requires input %s", n.Name, p.Title)
				}
			}
		}
	}
	queue := []string{}
	for _, n := range g.Nodes {
		if degree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	order := []string{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, next := range adj[id] {
			degree[next]--
			if degree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if len(order) != len(g.Nodes) {
		return nil, fmt.Errorf("cycles require an explicit iteration plugin")
	}
	return order, nil
}
func SafeError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 500 {
		s = s[:500]
	}
	return strings.ReplaceAll(s, "\x00", "")
}
func JSON(v any) []byte { b, _ := json.Marshal(v); return b }

func compileManifest(m plugin.Manifest) error {
	schemas := []map[string]any{m.ConfigSchema}
	for _, p := range append(m.Inputs, m.Outputs...) {
		if p.Side != "" && p.Side != "left" && p.Side != "right" && p.Side != "top" && p.Side != "bottom" {
			return fmt.Errorf("invalid port side %q", p.Side)
		}
		schemas = append(schemas, p.Schema)
	}
	for _, schema := range schemas {
		if schema == nil {
			return fmt.Errorf("schema required")
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource("urn:workflow:schema", schema); err != nil {
			return err
		}
		if _, err := c.Compile("urn:workflow:schema"); err != nil {
			return err
		}
	}
	return nil
}
