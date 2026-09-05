// Package plugin defines the language-neutral workflow extension protocol.
package plugin

import (
	"encoding/hex"
	"fmt"
	"regexp"
)

const Protocol = "workflow.plugin/v1"

type Schema = map[string]any

type Port struct {
	Side         string `json:"side,omitempty"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	ResourceType string `json:"resourceType,omitempty"`
	Required     bool   `json:"required,omitempty"`
	Sensitive    bool   `json:"sensitive,omitempty"`
	Multiple     bool   `json:"multiple,omitempty"`
	Schema       Schema `json:"schema"`
}
type Metric struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Unit        string `json:"unit"`
	Description string `json:"description"`
}
type Manifest struct {
	Protocol     string   `json:"protocol"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Category     string   `json:"category"`
	Kind         string   `json:"kind"`
	ConfigSchema Schema   `json:"configSchema"`
	Inputs       []Port   `json:"inputs"`
	Outputs      []Port   `json:"outputs"`
	Permissions  []string `json:"permissions"`
	Metrics      []Metric `json:"metrics,omitempty"`
	LogFields    []string `json:"logFields,omitempty"`
	Digest       string   `json:"digest"`
}
type Node struct {
	ResponsePort string         `json:"responsePort,omitempty"`
	ID           string         `json:"id"`
	Plugin       string         `json:"plugin"`
	Version      string         `json:"version"`
	Name         string         `json:"name"`
	Config       map[string]any `json:"config"`
	LogFields    []string       `json:"logFields,omitempty"`
	Position     struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"position"`
}
type Edge struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	SourcePort string `json:"sourcePort"`
	Target     string `json:"target"`
	TargetPort string `json:"targetPort"`
}
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}
type ExecutionContext struct {
	WorkflowID     string `json:"workflowId"`
	WorkflowName   string `json:"workflowName"`
	Revision       int    `json:"revision"`
	RunID          string `json:"runId"`
	NodeID         string `json:"nodeId"`
	NodeName       string `json:"nodeName"`
	Attempt        int    `json:"attempt"`
	IdempotencyKey string `json:"idempotencyKey"`
	TraceParent    string `json:"traceparent"`
	TraceState     string `json:"tracestate,omitempty"`
}
type Request struct {
	Protocol  string           `json:"protocol"`
	Plugin    string           `json:"plugin"`
	Version   string           `json:"version"`
	Digest    string           `json:"digest"`
	Context   ExecutionContext `json:"context"`
	Config    map[string]any   `json:"config"`
	Inputs    map[string]any   `json:"inputs"`
	LogFields []string         `json:"logFields"`
}
type Fault struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
type Response struct {
	Protocol string         `json:"protocol"`
	Outputs  map[string]any `json:"outputs"`
	Error    *Fault         `json:"error,omitempty"`
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,100}$`)
var version = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func (m Manifest) Validate() error {
	if m.Protocol != Protocol || !identifier.MatchString(m.Name) || !version.MatchString(m.Version) || m.Title == "" {
		return fmt.Errorf("invalid plugin identity or protocol")
	}
	if m.Kind != "action" && m.Kind != "resource" && m.Kind != "trigger" {
		return fmt.Errorf("invalid plugin kind")
	}
	if _, err := hex.DecodeString(m.Digest); err != nil || len(m.Digest) != 64 {
		return fmt.Errorf("plugin requires a SHA-256 artifact digest")
	}
	for _, ports := range [][]Port{m.Inputs, m.Outputs} {
		seen := map[string]bool{}
		for _, p := range ports {
			if !identifier.MatchString(p.Name) || seen[p.Name] {
				return fmt.Errorf("invalid or duplicate port %q", p.Name)
			}
			seen[p.Name] = true
			if p.Kind != "data" && p.Kind != "resource" {
				return fmt.Errorf("invalid port kind")
			}
			if p.Kind == "resource" && p.ResourceType == "" {
				return fmt.Errorf("resource type required")
			}
			if p.Schema == nil {
				return fmt.Errorf("port schema required")
			}
		}
	}
	metrics := map[string]bool{}
	for _, item := range m.Metrics {
		if !identifier.MatchString(item.Name) || metrics[item.Name] || (item.Kind != "counter" && item.Kind != "histogram") {
			return fmt.Errorf("invalid metric declaration")
		}
		metrics[item.Name] = true
	}
	fields := map[string]bool{}
	for _, field := range m.LogFields {
		if !identifier.MatchString(field) || fields[field] {
			return fmt.Errorf("invalid diagnostic field")
		}
		fields[field] = true
	}
	return nil
}
