package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brian-nunez/app-builder/sdk/go/plugin"
	"github.com/brian-nunez/bdb"
	"github.com/google/uuid"
	"log/slog"
)

//go:embed schema.sql
var schema string
var ErrConflict = errors.New("workflow changed; reload before saving")

type Store struct {
	DB     bdb.DB
	Cipher *Cipher
}
type Workflow struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Head      int          `json:"head"`
	Published *int         `json:"published"`
	UpdatedAt string       `json:"updatedAt"`
	Graph     plugin.Graph `json:"graph"`
}
type Revision struct {
	Revision  int          `json:"revision"`
	Name      string       `json:"name"`
	Author    string       `json:"author"`
	Message   string       `json:"message"`
	CreatedAt string       `json:"createdAt"`
	Graph     plugin.Graph `json:"graph"`
}

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(83918402)"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Catalog(ctx context.Context) (map[string]plugin.Manifest, error) {
	rows, err := s.DB.Query(ctx, "SELECT manifest FROM plugin_versions ORDER BY name,version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]plugin.Manifest{}
	for rows.Next() {
		var raw []byte
		var m plugin.Manifest
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		out[m.Name+"@"+m.Version] = m
	}
	return out, rows.Err()
}

// ErrPinnedContract reports a plugin release whose declared contract changed
// while a saved revision still pins it.
var ErrPinnedContract = errors.New("plugin contract changed for a version pinned by a saved revision")

// Register installs one plugin release.
//
// A version that no saved revision references may be refreshed freely, which is
// what makes local iteration comfortable. Once a revision pins a version, its
// contract is frozen: changing ports or schemas underneath a saved workflow would
// silently alter what that workflow does, so it is refused. Rebuilding the same
// contract from changed source is allowed and only moves the digest.
func (s *Store) Register(ctx context.Context, m plugin.Manifest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := compileManifest(m); err != nil {
		return err
	}
	var raw []byte
	err := s.DB.QueryOne(ctx, "SELECT manifest FROM plugin_versions WHERE name=$1 AND version=$2", []any{m.Name, m.Version}, &raw)
	if err != nil && !IsMissing(err) {
		return err
	}
	if err == nil {
		var existing plugin.Manifest
		if err = json.Unmarshal(raw, &existing); err != nil {
			return err
		}
		if existing.Contract() == m.Contract() {
			if existing.Digest == m.Digest {
				return nil
			}
			slog.WarnContext(ctx, "plugin artifact rebuilt", "plugin", m.Key(), "digest", m.Digest)
		} else {
			pinned, err := s.versionPinned(ctx, m.Name, m.Version)
			if err != nil {
				return err
			}
			if pinned {
				return fmt.Errorf("%w: %s; publish the change as a new version", ErrPinnedContract, m.Key())
			}
			slog.WarnContext(ctx, "plugin contract replaced", "plugin", m.Key())
		}
	}
	_, err = s.DB.Exec(ctx, "INSERT INTO plugin_versions(name,version,manifest) VALUES($1,$2,$3) ON CONFLICT(name,version) DO UPDATE SET manifest=EXCLUDED.manifest,created_at=now()", m.Name, m.Version, string(JSON(m)))
	return err
}

func (s *Store) versionPinned(ctx context.Context, name, version string) (bool, error) {
	var pinned bool
	err := s.DB.QueryOne(ctx, `SELECT EXISTS(SELECT 1 FROM revisions r CROSS JOIN LATERAL jsonb_array_elements(COALESCE(r.graph->'nodes','[]'::jsonb)) node WHERE node->>'plugin'=$1 AND node->>'version'=$2)`, []any{name, version}, &pinned)
	return pinned, err
}

// Uninstall removes a plugin release that is no longer present on disk. A release
// pinned by a saved revision is kept so that revision stays readable.
func (s *Store) Uninstall(ctx context.Context, name, version string) error {
	pinned, err := s.versionPinned(ctx, name, version)
	if err != nil {
		return err
	}
	if pinned {
		slog.WarnContext(ctx, "installed plugin missing but pinned by a saved revision", "plugin", name+"@"+version)
		return nil
	}
	_, err = s.DB.Exec(ctx, "DELETE FROM plugin_versions WHERE name=$1 AND version=$2", name, version)
	return err
}
func (s *Store) List(ctx context.Context) ([]Workflow, error) {
	rows, err := s.DB.Query(ctx, "SELECT id,name,head,published,updated_at::text FROM workflows ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workflow{}
	for rows.Next() {
		var w Workflow
		if err = rows.Scan(&w.ID, &w.Name, &w.Head, &w.Published, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (s *Store) Get(ctx context.Context, id string) (Workflow, error) {
	var w Workflow
	var raw []byte
	err := s.DB.QueryOne(ctx, "SELECT w.id,w.name,w.head,w.published,w.updated_at::text,r.graph FROM workflows w JOIN revisions r ON r.workflow_id=w.id AND r.revision=w.head WHERE w.id=$1", []any{id}, &w.ID, &w.Name, &w.Head, &w.Published, &w.UpdatedAt, &raw)
	if err == nil {
		w.Graph, err = s.decodeGraph(raw, id, w.Head)
	}
	return w, err
}
func (s *Store) Save(ctx context.Context, id, name string, base int, g plugin.Graph, actor, message string) (Workflow, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Workflow{}, err
	}
	defer tx.Rollback()
	next := base + 1
	if id == "" {
		id = uuid.NewString()
		next = 1
		if _, err = tx.ExecContext(ctx, "INSERT INTO workflows(id,name) VALUES($1,$2)", id, name); err != nil {
			return Workflow{}, err
		}
	} else {
		result, err := tx.ExecContext(ctx, "UPDATE workflows SET name=$2,head=head+1,updated_at=now() WHERE id=$1 AND head=$3", id, name, base)
		if err != nil {
			return Workflow{}, err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return Workflow{}, ErrConflict
		}
	}
	storedGraph, err := s.encodeGraph(g, id, next)
	if err != nil {
		return Workflow{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO revisions(workflow_id,revision,name,graph,author,message) VALUES($1,$2,$3,$4,$5,$6)", id, next, name, string(JSON(storedGraph)), actor, message); err != nil {
		return Workflow{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_events(actor,action,workflow_id,details) VALUES($1,'workflow.save',$2,$3)", actor, id, string(JSON(map[string]any{"revision": next, "message": message}))); err != nil {
		return Workflow{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workflow{}, err
	}
	return s.Get(ctx, id)
}
func (s *Store) History(ctx context.Context, id string) ([]Revision, error) {
	rows, err := s.DB.Query(ctx, "SELECT revision,name,author,message,created_at::text,graph FROM revisions WHERE workflow_id=$1 ORDER BY revision DESC LIMIT 200", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		var raw []byte
		if err = rows.Scan(&r.Revision, &r.Name, &r.Author, &r.Message, &r.CreatedAt, &raw); err != nil {
			return nil, err
		}
		if r.Graph, err = s.decodeGraph(raw, id, r.Revision); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Revision(ctx context.Context, id string, revision int) (Revision, error) {
	var r Revision
	var raw []byte
	err := s.DB.QueryOne(ctx, "SELECT revision,name,author,message,created_at::text,graph FROM revisions WHERE workflow_id=$1 AND revision=$2", []any{id, revision}, &r.Revision, &r.Name, &r.Author, &r.Message, &r.CreatedAt, &raw)
	if err == nil {
		r.Graph, err = s.decodeGraph(raw, id, r.Revision)
	}
	return r, err
}
func (s *Store) Publish(ctx context.Context, id string, head int, actor string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE workflows SET published=$2 WHERE id=$1 AND head=$2", id, head)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_events(actor,action,workflow_id) VALUES($1,'workflow.publish',$2)", actor, id); err != nil {
		return err
	}
	return tx.Commit()
}

type Run struct {
	ID          string  `json:"id"`
	WorkflowID  string  `json:"workflowId"`
	Revision    int     `json:"revision"`
	Status      string  `json:"status"`
	Attempt     int     `json:"attempt"`
	Error       *string `json:"error"`
	CreatedAt   string  `json:"createdAt"`
	FinishedAt  *string `json:"finishedAt"`
	LeaseToken  string  `json:"-"`
	Input       []byte  `json:"-"`
	TraceParent string  `json:"-"`
}

type RunDetail struct {
	Run
	Input  any `json:"input"`
	Output any `json:"output"`
}

func (s *Store) Enqueue(ctx context.Context, id string, revision int, input any, key, traceparent string) (string, error) {
	runID := uuid.NewString()
	sealed, err := s.Cipher.Seal(input, id)
	if err != nil {
		return "", err
	}
	var k any
	if key != "" {
		k = key
	}
	err = s.DB.QueryOne(ctx, "INSERT INTO runs(id,workflow_id,revision,status,input,idempotency_key,traceparent) VALUES($1,$2,$3,'queued',$4,$5,$6) ON CONFLICT(workflow_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING id", []any{runID, id, revision, sealed, k, traceparent}, &runID)
	return runID, err
}
func (s *Store) Runs(ctx context.Context, id string) ([]Run, error) {
	rows, err := s.DB.Query(ctx, "SELECT id,workflow_id,revision,status,attempt,error,created_at::text,finished_at::text FROM runs WHERE workflow_id=$1 ORDER BY created_at DESC LIMIT 100", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		if err = rows.Scan(&r.ID, &r.WorkflowID, &r.Revision, &r.Status, &r.Attempt, &r.Error, &r.CreatedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) RunDetail(ctx context.Context, id string) (RunDetail, error) {
	var detail RunDetail
	var input, output []byte
	err := s.DB.QueryOne(ctx, "SELECT id,workflow_id,revision,status,attempt,error,created_at::text,finished_at::text,input,output FROM runs WHERE id=$1", []any{id}, &detail.ID, &detail.WorkflowID, &detail.Revision, &detail.Status, &detail.Attempt, &detail.Error, &detail.CreatedAt, &detail.FinishedAt, &input, &output)
	if err != nil {
		return RunDetail{}, err
	}
	if err = s.Cipher.Open(input, detail.WorkflowID, &detail.Input); err != nil {
		return RunDetail{}, err
	}
	if len(output) > 0 {
		if err = s.Cipher.Open(output, detail.ID, &detail.Output); err != nil {
			return RunDetail{}, err
		}
	}
	return detail, nil
}

func (s *Store) DeleteWorkflow(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM steps WHERE run_id IN (SELECT id FROM runs WHERE workflow_id=$1)", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM trigger_bindings WHERE workflow_id=$1", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM runs WHERE workflow_id=$1", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM revisions WHERE workflow_id=$1", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM audit_events WHERE workflow_id=$1", id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM workflows WHERE id=$1", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}
func (s *Store) Claim(ctx context.Context) (Run, error) {
	var r Run
	token := uuid.NewString()
	err := s.DB.QueryOne(ctx, `WITH candidate AS (SELECT id FROM runs WHERE (status='queued' AND next_attempt_at<=now()) OR (status='running' AND lease_until<now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE runs SET status='running',attempt=attempt+1,lease_token=$1,lease_until=now()+interval '45 seconds' FROM candidate WHERE runs.id=candidate.id RETURNING runs.id,workflow_id,revision,attempt,input,traceparent`, []any{token}, &r.ID, &r.WorkflowID, &r.Revision, &r.Attempt, &r.Input, &r.TraceParent)
	r.LeaseToken = token
	return r, err
}
func (s *Store) Heartbeat(ctx context.Context, r Run) error {
	res, err := s.DB.Exec(ctx, "UPDATE runs SET lease_until=now()+interval '45 seconds' WHERE id=$1 AND lease_token=$2 AND status='running'", r.ID, r.LeaseToken)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return context.Canceled
	}
	return nil
}
func (s *Store) Complete(ctx context.Context, r Run, status string, output any, message string, retry bool) error {
	sealed, err := s.Cipher.Seal(output, r.ID)
	if err != nil {
		return err
	}
	if retry && r.Attempt < 3 {
		status = "queued"
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE runs SET status=$3,output=$4,error=NULLIF($5,''),finished_at=CASE WHEN $3='queued' THEN NULL ELSE now() END,next_attempt_at=now()+make_interval(secs=>$6),lease_until=NULL WHERE id=$1 AND lease_token=$2 AND status='running'", r.ID, r.LeaseToken, status, sealed, message, r.Attempt*5)
	if err = requireLease(result, err); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE steps SET status='failed',error=NULLIF($3,''),finished_at=now() WHERE run_id=$1 AND attempt=$2 AND status='running'", r.ID, r.Attempt, message); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) CachedStep(ctx context.Context, runID, node string) (map[string]any, error) {
	var data []byte
	err := s.DB.QueryOne(ctx, "SELECT output FROM steps WHERE run_id=$1 AND node_id=$2 AND status='succeeded' ORDER BY attempt DESC LIMIT 1", []any{runID, node}, &data)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = s.Cipher.Open(data, runID+":"+node, &out)
	return out, err
}
func (s *Store) StartStep(ctx context.Context, r Run, node string) error {
	result, err := s.DB.Exec(ctx, "INSERT INTO steps(run_id,node_id,attempt,status) SELECT id,$2,$3,'running' FROM runs WHERE id=$1 AND lease_token=$4 AND status='running' AND lease_until>now()", r.ID, node, r.Attempt, r.LeaseToken)
	return requireLease(result, err)
}
func (s *Store) EndStep(ctx context.Context, r Run, node, status string, out any, message string) error {
	data, err := s.Cipher.Seal(out, r.ID+":"+node)
	if err != nil {
		return err
	}
	result, err := s.DB.Exec(ctx, "UPDATE steps SET status=$4,output=$5,error=NULLIF($6,''),finished_at=now() WHERE run_id=$1 AND node_id=$2 AND attempt=$3 AND EXISTS(SELECT 1 FROM runs WHERE id=$1 AND lease_token=$7 AND status='running' AND lease_until>now())", r.ID, node, r.Attempt, status, data, message, r.LeaseToken)
	return requireLease(result, err)
}
func requireLease(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return context.Canceled
	}
	return nil
}
func (s *Store) Cancel(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE runs SET status='cancelled',finished_at=now() WHERE id=$1 AND status IN ('queued','running')", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE steps SET status='cancelled',finished_at=now() WHERE run_id=$1 AND status='running'", id); err != nil {
		return err
	}
	return tx.Commit()
}
func IsMissing(err error) bool { return errors.Is(err, sql.ErrNoRows) }

type Step struct {
	NodeID     string  `json:"nodeId"`
	Attempt    int     `json:"attempt"`
	Status     string  `json:"status"`
	Error      *string `json:"error"`
	StartedAt  string  `json:"startedAt"`
	FinishedAt *string `json:"finishedAt"`
}

// Binding is a published inbound endpoint for one trigger node.
type Binding struct {
	ID         string  `json:"id"`
	NodeID     string  `json:"nodeId"`
	Mode       string  `json:"mode"`
	Path       string  `json:"path"`
	Token      string  `json:"token"`
	RunID      *string `json:"runId"`
	ReceivedAt *string `json:"receivedAt"`
}

func (s *Store) Steps(ctx context.Context, runID string) ([]Step, error) {
	rows, err := s.DB.Query(ctx, "SELECT node_id,attempt,status,error,started_at::text,finished_at::text FROM steps WHERE run_id=$1 ORDER BY started_at", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Step{}
	for rows.Next() {
		var step Step
		if err = rows.Scan(&step.NodeID, &step.Attempt, &step.Status, &step.Error, &step.StartedAt, &step.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, rows.Err()
}

func (s *Store) Bindings(ctx context.Context, workflowID string) ([]Binding, error) {
	rows, err := s.DB.Query(ctx, "SELECT id,node_id,mode,token_cipher,last_run_id::text,received_at::text FROM trigger_bindings WHERE workflow_id=$1 ORDER BY created_at DESC", workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Binding{}
	for rows.Next() {
		var binding Binding
		var sealed []byte
		if err = rows.Scan(&binding.ID, &binding.NodeID, &binding.Mode, &sealed, &binding.RunID, &binding.ReceivedAt); err != nil {
			return nil, err
		}
		if len(sealed) > 0 {
			// Endpoints created before tokens were retained keep only their hash.
			if err = s.Cipher.Open(sealed, "binding:"+binding.ID, &binding.Token); err != nil {
				return nil, err
			}
		}
		binding.Path = "/hooks/" + binding.ID
		out = append(out, binding)
	}
	return out, rows.Err()
}

// CreateBinding issues an inbound endpoint and its bearer token.
func (s *Store) CreateBinding(ctx context.Context, workflowID, nodeID, mode, token string) (Binding, error) {
	id := uuid.NewString()
	sealed, err := s.Cipher.Seal(token, "binding:"+id)
	if err != nil {
		return Binding{}, err
	}
	_, err = s.DB.Exec(ctx, "INSERT INTO trigger_bindings(id,workflow_id,node_id,token_hash,mode,token_cipher) VALUES($1,$2,$3,$4,$5,$6)", id, workflowID, nodeID, TokenHash(token), mode, sealed)
	if err != nil {
		return Binding{}, err
	}
	return Binding{ID: id, NodeID: nodeID, Mode: mode, Path: "/hooks/" + id, Token: token}, nil
}

// Endpoint is a trigger binding resolved to the revision it currently targets.
type Endpoint struct {
	WorkflowID string
	NodeID     string
	TokenHash  string
	Revision   *int
}

func (s *Store) Endpoint(ctx context.Context, bindingID string) (Endpoint, error) {
	var e Endpoint
	err := s.DB.QueryOne(ctx, "SELECT b.workflow_id,b.node_id,b.token_hash,CASE WHEN b.mode='draft' THEN w.head ELSE w.published END FROM trigger_bindings b JOIN workflows w ON w.id=b.workflow_id WHERE b.id=$1", []any{bindingID}, &e.WorkflowID, &e.NodeID, &e.TokenHash, &e.Revision)
	return e, err
}

func (s *Store) RecordBindingRun(ctx context.Context, bindingID, runID string) error {
	_, err := s.DB.Exec(ctx, "UPDATE trigger_bindings SET last_run_id=$2,received_at=now() WHERE id=$1", bindingID, runID)
	return err
}

// RunOutcome is the part of a run a webhook caller is allowed to observe.
type RunOutcome struct {
	Status   string
	Revision int
	Output   []byte
}

func (s *Store) RunOutcome(ctx context.Context, runID, workflowID string) (RunOutcome, error) {
	var outcome RunOutcome
	err := s.DB.QueryOne(ctx, "SELECT status,revision,output FROM runs WHERE id=$1 AND workflow_id=$2", []any{runID, workflowID}, &outcome.Status, &outcome.Revision, &outcome.Output)
	return outcome, err
}

// TokenHash is the stored form of an inbound endpoint's bearer token.
func TokenHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
