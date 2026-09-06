package server

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) hookResult(w http.ResponseWriter, r *http.Request) {
	var workflowID, hash string
	err := s.store.DB.QueryOne(r.Context(), "SELECT workflow_id,token_hash FROM trigger_bindings WHERE id=$1", []any{r.PathValue("id")}, &workflowID, &hash)
	if err != nil {
		respond(w, 404, map[string]string{"error": "Webhook endpoint not found"})
		return
	}
	provided := tokenHash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if subtle.ConstantTimeCompare([]byte(hash), []byte(provided)) != 1 {
		respond(w, 401, map[string]string{"error": "Missing or invalid webhook bearer token"})
		return
	}
	s.sendHookResult(w, r, r.PathValue("runId"), workflowID, false)
}

func (s *Server) sendHookResult(w http.ResponseWriter, r *http.Request, runID, workflowID string, wait bool) {
	w.Header().Set("Cache-Control", "no-store")
	location := "/hooks/" + r.PathValue("id") + "/runs/" + runID
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status string
		var revision int
		var sealed []byte
		err := s.store.DB.QueryOne(r.Context(), "SELECT status,revision,output FROM runs WHERE id=$1 AND workflow_id=$2", []any{runID, workflowID}, &status, &revision, &sealed)
		if err != nil {
			respond(w, 404, map[string]string{"error": "Run not found"})
			return
		}
		result := map[string]any{"runId": runID, "status": status, "statusUrl": location}
		switch status {
		case "succeeded":
			rev, err := s.store.Revision(r.Context(), workflowID, revision)
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
				if err := s.store.Cipher.Open(sealed, runID, &outputs); err != nil {
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
			result["error"] = "Workflow " + status
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
