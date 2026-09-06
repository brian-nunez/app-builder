# Local verification

Verified on this machine during implementation:

- Go compilation and `go vet ./...`.
- Vite/TypeScript production build and frontend ESLint.
- All installed plugin manifests and SHA-256 artifact digests.
- PostgreSQL 18, Go platform, Python worker, and OpenTelemetry Collector container health.
- Authenticated workflow creation, revision saves, and HTTP 409 rejection of stale edits.
- Single-node restoration creating a new revision with the previous configuration.
- Browser autosave returning HTTP 200 and showing Saved state.
- Encrypted node configuration in PostgreSQL and successful decryption on API reads and execution.
- A connected static configuration → JSON-field plugin run completing successfully.
- Authenticated webhook acceptance and execution of the published revision while a newer draft exists.
- Core execution logs, traces, and metrics arriving at the Collector with workflow/plugin attributes.
- A plugin-defined `json.fields.resolved` counter, `json.resolve` child span, and opted-in `value_type` diagnostic field arriving at the Collector.
- Dashboard/editor loading in desktop Edge without browser runtime errors; desktop and mobile inspector layout inspected.

The local `Configuration check` workflow and its history remain available for inspection. Earlier failed verification runs remain visible rather than rewriting execution history.

Not yet verified against real services:

- Keycloak browser login and role enforcement from a second Tailscale device.
- On-premises One Data/OpenAPI services and their authentication requirements.
- Real Vault/Consul endpoints and enterprise certificates.
- Real model/MCP endpoints and an independently provisioned PostgreSQL memory database.
- Multi-replica failover, sustained load, and hostile-plugin isolation.

No automated tests have been written. Unit, integration, and browser test suites require the repository owner's explicit approval before authoring.

## Usability repair verification

After the initial delivery, the following checks were performed through the UI and local curl:

- Created a separate resource-selection workflow through the browser.
- Selected a model provider, PostgreSQL memory, a prompt source, and two MCP sources through Inputs & resources; all five connections persisted.
- Dragged the incoming webhook's output to a JSON-field input and verified the edge persisted.
- Created a test endpoint without publishing, sent the browser's test POST, and observed the received run.
- Sent a local curl POST through the two-node workflow; both steps succeeded.
- Published that workflow and created its live endpoint through the browser.
- Confirmed HTTP 401 for a missing bearer token and HTTP 405 for a non-POST webhook request, with explanatory JSON errors.
- Measured 85 visible editor text elements after the contrast update; the minimum computed ratio was 6.16:1, with none below 4.5:1. This is a check of that visible screen, not a full accessibility certification.

Resource selection is verified. Actual provider execution still needs real model, memory, and MCP service configuration. No claim of production readiness is made by these local checks.
