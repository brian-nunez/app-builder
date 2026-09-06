package server

import (
	"crypto/subtle"
	"fmt"
	"github.com/brian-nunez/app-builder/internal/platform"
	"net/http"
	"strings"
	"time"
)

func (s *Server) hookResult(w http.ResponseWriter, r *http.Request) {
	endpoint, err := s.store.Endpoint(r.Context(), r.PathValue("id"))
	if err != nil {
		respond(w, 404, map[string]string{"error": "Webhook endpoint not found"})
		return
	}
	provided := platform.TokenHash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if subtle.ConstantTimeCompare([]byte(endpoint.TokenHash), []byte(provided)) != 1 {
		respond(w, 401, map[string]string{"error": "Missing or invalid webhook bearer token"})
		return
	}
	s.sendHookResult(w, r, r.PathValue("runId"), endpoint.WorkflowID, false)
}

func (s *Server) sendHookResult(w http.ResponseWriter, r *http.Request, runID, workflowID string, wait bool) {
	w.Header().Set("Cache-Control", "no-store")
	location := "/hooks/" + r.PathValue("id") + "/runs/" + runID
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		outcome, err := s.store.RunOutcome(r.Context(), runID, workflowID)
		if err != nil {
			respond(w, 404, map[string]string{"error": "Run not found"})
			return
		}
		result := map[string]any{"runId": runID, "status": outcome.Status, "statusUrl": location}
		switch outcome.Status {
		case "succeeded":
			rev, err := s.store.Revision(r.Context(), workflowID, outcome.Revision)
			if err != nil {
				fail(w, err)
				return
			}
			// Only the revision's explicitly exposed output crosses the webhook boundary.
			for _, node := range rev.Graph.Nodes {
				if node.ResponsePort == "" {
					continue
				}
				var outputs map[string]map[string]any
				if err := s.store.Cipher.Open(outcome.Output, runID, &outputs); err != nil {
					fail(w, err)
					return
				}
				value, ok := outputs[node.ID][node.ResponsePort]
				if !ok {
					fail(w, fmt.Errorf("configured workflow response was not produced"))
					return
				}
				result["response"] = value
			}
			respond(w, 200, result)
			return
		case "failed", "cancelled":
			result["error"] = "Workflow " + outcome.Status
			respond(w, 422, result)
			return
		}
		if !wait {
			w.Header().Set("Location", location)
			w.Header().Set("Retry-After", "2")
			respond(w, 202, result)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			wait = false
		case <-ticker.C:
		}
	}
}
