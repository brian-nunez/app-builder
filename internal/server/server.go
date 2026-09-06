package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brian-nunez/app-builder/internal/platform"
	"github.com/brian-nunez/app-builder/sdk/go/plugin"
	"github.com/brian-nunez/bhttp/pkg/bsuite"
	"github.com/coreos/go-oidc/v3/oidc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"golang.org/x/oauth2"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Server struct {
	store           *platform.Store
	auth            *auth
	address, assets string
}

func New(ctx context.Context, service *bsuite.Service, store *platform.Store) (*Server, error) {
	cfg := service.Config()
	a := &auth{cipher: store.Cipher, token: cfg.String("security.access_token"), secure: cfg.Bool("security.secure_cookies")}
	if issuer := cfg.String("oidc.issuer"); issuer != "" {
		provider, err := oidc.NewProvider(ctx, issuer)
		if err != nil {
			return nil, err
		}
		clientID := cfg.String("oidc.client_id")
		a.oauth = &oauth2.Config{ClientID: clientID, ClientSecret: cfg.String("oidc.client_secret"), RedirectURL: cfg.String("oidc.redirect_url"), Endpoint: provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile"}}
		a.verifier = provider.Verifier(&oidc.Config{ClientID: clientID})
	} else if len(a.token) < 32 {
		return nil, fmt.Errorf("configure OIDC or an access token with at least 32 characters")
	}
	return &Server{store: store, auth: a, address: cfg.String("server.address"), assets: cfg.String("server.assets")}, nil
}
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	api := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if s.store.DB.Ping(c) != nil {
			http.Error(w, "Database unavailable", 503)
			return
		}
		respond(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("/auth/login", s.auth.login)
	mux.HandleFunc("GET /auth/callback", s.auth.callback)
	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.auth.secure})
		respond(w, 200, map[string]bool{"ok": true})
	})
	api.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"actor": actor(r)}) })
	api.HandleFunc("GET /api/plugins", func(w http.ResponseWriter, r *http.Request) {
		catalog, err := s.store.Catalog(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, catalog)
	})
	api.HandleFunc("GET /api/workflows", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.List(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, items)
	})
	api.HandleFunc("POST /api/workflows", s.save)
	api.HandleFunc("PUT /api/workflows/{id}", s.save)
	api.HandleFunc("DELETE /api/workflows/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.store.DeleteWorkflow(r.Context(), r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	api.HandleFunc("GET /api/workflows/{id}", func(w http.ResponseWriter, r *http.Request) {
		item, err := s.store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, item)
	})
	api.HandleFunc("GET /api/workflows/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.History(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, items)
	})
	api.HandleFunc("POST /api/workflows/{id}/restore", s.restore)
	api.HandleFunc("POST /api/workflows/{id}/publish", s.publish)
	api.HandleFunc("POST /api/workflows/{id}/runs", s.run)
	api.HandleFunc("GET /api/workflows/{id}/runs", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.Runs(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, items)
	})
	api.HandleFunc("GET /api/runs/{id}/steps", s.steps)
	api.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		item, err := s.store.RunDetail(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		respond(w, 200, item)
	})
	api.HandleFunc("POST /api/runs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if err := s.store.Cancel(r.Context(), r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	api.HandleFunc("POST /api/workflows/{id}/bindings", s.bind)
	api.HandleFunc("GET /api/workflows/{id}/bindings", s.bindings)
	mux.Handle("/api/", s.auth.protect(api))
	mux.HandleFunc("POST /hooks/{id}", s.hook)
	mux.HandleFunc("GET /hooks/{id}/runs/{runId}", s.hookResult)
	mux.HandleFunc("/hooks/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "POST")
		respond(w, http.StatusMethodNotAllowed, map[string]string{"error": "Webhook endpoints accept POST requests with a JSON body and bearer token"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(s.assets, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFile(w, r, path)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(s.assets, "index.html"))
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				slog.Error("request panic")
				http.Error(w, "Internal server error", 500)
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		if r.Method != "GET" && r.Method != "HEAD" {
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
				http.Error(w, "Cross-site request rejected", 403)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				http.Error(w, "Origin rejected", 403)
				return
			}
		}
		c := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		c, span := otel.Tracer("workflow.http").Start(c, "http.request")
		defer span.End()
		mux.ServeHTTP(w, r.WithContext(c))
	})
	server := &http.Server{Addr: s.address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	slog.Info("HTTP service started", "address", s.address)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid request body")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("request must contain one JSON value")
	}
	return nil
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("response encoding failed", "error", err)
	}
}
func fail(w http.ResponseWriter, err error) {
	status := 400
	message := platform.SafeError(err)
	if errors.Is(err, platform.ErrConflict) {
		status = 409
	}
	if platform.IsMissing(err) {
		status = 404
		message = "Not found"
	}
	if strings.Contains(message, "pq:") || strings.Contains(message, "sql:") {
		slog.Error("database request failed", "error", err)
		status = 500
		message = "Database operation failed"
	}
	respond(w, status, map[string]string{"error": message})
}
func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string       `json:"name"`
		Base    int          `json:"base"`
		Graph   plugin.Graph `json:"graph"`
		Message string       `json:"message"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if len(body.Name) < 1 || len(body.Name) > 160 {
		fail(w, fmt.Errorf("workflow name must be 1–160 characters"))
		return
	}
	catalog, err := s.store.Catalog(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if _, err = platform.ValidateGraph(body.Graph, catalog, false); err != nil {
		fail(w, err)
		return
	}
	item, err := s.store.Save(r.Context(), r.PathValue("id"), body.Name, body.Base, body.Graph, actor(r), body.Message)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, item)
}
func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Revision int    `json:"revision"`
		Base     int    `json:"base"`
		NodeID   string `json:"nodeId"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	id := r.PathValue("id")
	old, err := s.store.Revision(r.Context(), id, body.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	graph := old.Graph
	name := old.Name
	if body.NodeID != "" {
		current, err := s.store.Get(r.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		graph = current.Graph
		name = current.Name
		found := false
		for _, n := range old.Graph.Nodes {
			if n.ID == body.NodeID {
				for i, c := range graph.Nodes {
					if c.ID == n.ID {
						graph.Nodes[i] = n
						found = true
					}
				}
			}
		}
		if !found {
			fail(w, fmt.Errorf("node must exist in both revisions"))
			return
		}
	}
	catalog, err := s.store.Catalog(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if _, err = platform.ValidateGraph(graph, catalog, false); err != nil {
		fail(w, err)
		return
	}
	item, err := s.store.Save(r.Context(), id, name, body.Base, graph, actor(r), fmt.Sprintf("Restored revision %d", body.Revision))
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, item)
}
func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Base int `json:"base"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	item, err := s.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	catalog, err := s.store.Catalog(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if len(item.Graph.Nodes) == 0 {
		fail(w, fmt.Errorf("add a node before publishing"))
		return
	}
	if _, err = platform.ValidateGraph(item.Graph, catalog, true); err != nil {
		fail(w, err)
		return
	}
	if err = s.store.Publish(r.Context(), item.ID, body.Base, actor(r)); err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input map[string]any `json:"input"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	item, err := s.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	catalog, err := s.store.Catalog(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if len(item.Graph.Nodes) == 0 {
		fail(w, fmt.Errorf("add a node before running"))
		return
	}
	if _, err = platform.ValidateGraph(item.Graph, catalog, true); err != nil {
		fail(w, err)
		return
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(r.Context(), carrier)
	id, err := s.store.Enqueue(r.Context(), item.ID, item.Head, body.Input, r.Header.Get("Idempotency-Key"), carrier["traceparent"])
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 202, map[string]string{"id": id})
}
func (s *Server) bind(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"nodeId"`
		Mode   string `json:"mode"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	item, err := s.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if body.Mode == "" {
		body.Mode = "published"
	}
	if body.Mode != "draft" && body.Mode != "published" {
		fail(w, fmt.Errorf("invalid endpoint mode"))
		return
	}
	revisionNumber := item.Head
	if body.Mode == "published" {
		if item.Published == nil {
			fail(w, fmt.Errorf("publish the workflow to activate its live endpoint, or create a test endpoint"))
			return
		}
		revisionNumber = *item.Published
	}
	revision, err := s.store.Revision(r.Context(), item.ID, revisionNumber)
	if err != nil {
		fail(w, err)
		return
	}
	catalog, err := s.store.Catalog(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	found := false
	for _, n := range revision.Graph.Nodes {
		if n.ID == body.NodeID && catalog[n.Plugin+"@"+n.Version].Kind == "trigger" {
			found = true
		}
	}
	if !found {
		fail(w, fmt.Errorf("node is not a published trigger"))
		return
	}
	binding, err := s.store.CreateBinding(r.Context(), item.ID, body.NodeID, body.Mode, randomToken())
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 201, binding)
}
func (s *Server) hook(w http.ResponseWriter, r *http.Request) {
	endpoint, err := s.store.Endpoint(r.Context(), r.PathValue("id"))
	if err != nil {
		respond(w, 404, map[string]string{"error": "Webhook endpoint not found"})
		return
	}
	workflowID, nodeID, revision := endpoint.WorkflowID, endpoint.NodeID, endpoint.Revision
	provided := platform.TokenHash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if subtle.ConstantTimeCompare([]byte(endpoint.TokenHash), []byte(provided)) != 1 {
		respond(w, 401, map[string]string{"error": "Missing or invalid bearer token. Copy the curl command from the webhook settings."})
		return
	}
	if revision == nil {
		respond(w, 409, map[string]string{"error": "Publish the workflow before calling its live endpoint"})
		return
	}
	rev, err := s.store.Revision(r.Context(), workflowID, *revision)
	if err != nil {
		fail(w, err)
		return
	}
	catalog, err := s.store.Catalog(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	found := false
	for _, n := range rev.Graph.Nodes {
		if n.ID == nodeID && catalog[n.Plugin+"@"+n.Version].Kind == "trigger" {
			found = true
		}
	}
	if !found {
		respond(w, 409, map[string]string{"error": "This endpoint refers to a trigger that no longer exists in its workflow revision"})
		return
	}
	if _, err = platform.ValidateGraph(rev.Graph, catalog, true); err != nil {
		fail(w, err)
		return
	}
	var input map[string]any
	if err = decode(w, r, &input); err != nil {
		fail(w, err)
		return
	}

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(r.Context(), carrier)
	id, err := s.store.Enqueue(r.Context(), workflowID, *revision, input, r.Header.Get("Idempotency-Key"), carrier["traceparent"])
	if err != nil {
		fail(w, err)
		return
	}
	if err = s.store.RecordBindingRun(r.Context(), r.PathValue("id"), id); err != nil {
		slog.ErrorContext(r.Context(), "webhook receipt update failed", "error", err)
	}
	wait := false
	for _, node := range rev.Graph.Nodes {
		if node.ResponsePort != "" {
			wait = true
		}
	}
	if r.URL.Query().Get("wait") == "0" {
		wait = false
	}
	s.sendHookResult(w, r, id, workflowID, wait)
}
func (s *Server) steps(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Steps(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, items)
}

func (s *Server) bindings(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Bindings(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, 200, items)
}
