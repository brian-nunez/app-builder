# Architecture

## Separation of responsibilities

The browser renders React Flow and submits domain graphs to the Go API. It does not schedule steps, resolve credentials, invoke services, or provide authoritative state. Canvas coordinates are kept in node layout fields; execution only reads plugin identity, configuration, ports, and graph edges.

Go serves the Vite build, validates requests, authenticates users, versions workflow changes, publishes revisions, and coordinates execution. BKit provides configuration, dependency initialization, database connections, authorization predicates, telemetry traces/metrics, and concurrent lifecycle management. The OpenTelemetry log bridge supplies logging because the current BTelemetry API has no log provider.

The Python service validates and executes installed plugins. Each invocation starts a child process with a restricted environment and bounded duration. Inputs and outputs follow `workflow.plugin/v1`; resource descriptors use `workflow.resource/v1`. No Go package imports an integration plugin.

## Three independent database roles

1. **Platform PostgreSQL 18**: workflow definitions, plugin metadata, revisions, runs, steps, trigger bindings, audit history.
2. **Agent/service memory database**: selected through a resource plugin with explicit credentials. Its schema and lifecycle belong to that plugin.
3. **Application databases**: accessed by other plugins under their own contracts.

The worker container receives neither platform database credentials nor the platform encryption key. Host allowlists are configured separately for plugin destinations.

## Persistence and concurrency

A save runs in one transaction: compare the expected head, advance it, insert the immutable snapshot, and insert an audit event. A conflicting editor receives HTTP 409. Restore inserts another snapshot rather than updating history. Database triggers reject UPDATE/DELETE on revision and plugin-version rows.

Publishing changes a separate revision pointer. Trigger bindings always resolve that pointer, so unpublished edits do not change active webhook behavior. Runs reference a fixed immutable workflow revision. Plugin versions are pinned in that revision and metadata is immutable. Executable artifacts must be retained separately in worker images.

Run payloads, step outputs, and final outputs are AES-256-GCM encrypted with scope-bound associated data. New revision snapshots also encrypt each node configuration with workflow/revision/node-scoped associated data. Graph topology, plugin identities, names, and audit metadata remain ordinary JSON/text. Authorized workflow reads decrypt configuration; this is storage encryption, not per-field authorization. Encrypted outputs are retained for recovery and are not exposed through status endpoints.

## Execution semantics

A PostgreSQL `FOR UPDATE SKIP LOCKED` claim assigns a unique lease token. The scheduler renews the lease every ten seconds. Expired leases can be claimed after 45 seconds. Run completion requires the current lease token. Recovery uses completed action checkpoints, while resource plugins re-resolve credentials/configuration. Retries are bounded to three workflow attempts.

The initial scheduler processes acyclic dependency graphs in deterministic topological order. It validates required connections before publishing/running. Inputs are schema-validated again after materializing upstream results. Each step has an execution span and metrics with workflow and plugin identity. Resource hooks construct and close live objects inside a consuming invocation.

A crash between an external side effect and a committed checkpoint can repeat that effect. The SDK supplies a stable idempotency key per run/node. Plugins and target services are responsible for idempotency and reconciliation. Cancellation prevents additional steps and marks the run cancelled; it cannot roll back effects already sent to an external system.

## Security scope

The workspace is shared across authorized users. Keycloak roles are `workflow-admin`, `workflow-editor`, and `workflow-viewer`. Editors can mutate and run workflows; viewers can read; admins additionally create inbound bindings. Local token authentication represents one administrator. This release does not implement tenant isolation, per-workflow ACLs, or role-specific field masking.

Plugin installation is an operator action through filesystem packages and image deployment. The runtime is intended for reviewed code; it is not a marketplace sandbox. Private on-premises integrations are allowed through an explicit hostname allowlist. Network isolation outside the SDK is required for untrusted code.
