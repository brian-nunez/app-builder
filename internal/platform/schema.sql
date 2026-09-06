CREATE TABLE IF NOT EXISTS plugin_versions (
 name text NOT NULL, version text NOT NULL, manifest jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(name, version)
);
CREATE TABLE IF NOT EXISTS workflows (
 id uuid PRIMARY KEY, name text NOT NULL CHECK(length(name) BETWEEN 1 AND 160),
 head integer NOT NULL DEFAULT 1, published integer,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS revisions (
 workflow_id uuid NOT NULL REFERENCES workflows(id), revision integer NOT NULL,
 name text NOT NULL, graph jsonb NOT NULL, author text NOT NULL, message text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workflow_id, revision)
);
CREATE TABLE IF NOT EXISTS runs (
 id uuid PRIMARY KEY, workflow_id uuid NOT NULL, revision integer NOT NULL,
 status text NOT NULL CHECK(status IN ('queued','running','succeeded','failed','cancelled')),
 input bytea NOT NULL, output bytea, error text, attempt integer NOT NULL DEFAULT 0,
 lease_until timestamptz, lease_token uuid, next_attempt_at timestamptz NOT NULL DEFAULT now(),
 idempotency_key text, traceparent text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz,
 FOREIGN KEY(workflow_id,revision) REFERENCES revisions(workflow_id,revision), UNIQUE(workflow_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS runs_queue ON runs(next_attempt_at,created_at) WHERE status IN ('queued','running');
CREATE TABLE IF NOT EXISTS steps (
 run_id uuid NOT NULL REFERENCES runs(id), node_id text NOT NULL, attempt integer NOT NULL,
 status text NOT NULL, output bytea, error text, started_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz,
 PRIMARY KEY(run_id,node_id,attempt)
);
CREATE TABLE IF NOT EXISTS trigger_bindings (
 id uuid PRIMARY KEY, workflow_id uuid NOT NULL REFERENCES workflows(id), node_id text NOT NULL,
 token_hash text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, actor text NOT NULL, action text NOT NULL,
 workflow_id uuid, details jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION prevent_revision_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'revision and plugin version history is immutable'; END $$;
DROP TRIGGER IF EXISTS revisions_immutable ON revisions;
CREATE TRIGGER revisions_immutable BEFORE UPDATE ON revisions FOR EACH ROW EXECUTE FUNCTION prevent_revision_mutation();
-- Plugin version immutability is enforced in Store.Register, which allows a
-- version no revision pins to be refreshed and freezes one that is pinned.
ALTER TABLE trigger_bindings ADD COLUMN IF NOT EXISTS mode text NOT NULL DEFAULT 'published';
ALTER TABLE trigger_bindings ADD COLUMN IF NOT EXISTS token_cipher bytea;
ALTER TABLE trigger_bindings ADD COLUMN IF NOT EXISTS last_run_id uuid REFERENCES runs(id);
ALTER TABLE trigger_bindings ADD COLUMN IF NOT EXISTS received_at timestamptz;
