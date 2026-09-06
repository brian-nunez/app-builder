# Local Gemma and Consul workflow

Open **Local Gemma · Consul agent** at http://127.0.0.1:8080/ locally or `http://<tailscale-ip>:8080/` from another Tailscale device.
The published workflow has 11 nodes and 12 connections. All integrations remain installed plugins; the platform has no Consul, model, memory, or agent-specific execution logic.

## Request flow

1. Incoming webhook receives `model_id`, `thread_id`, and `message`.
2. JSON field nodes extract the model ID and message. The Thread ID plugin preserves a supplied nonempty string or generates a UUIDv7 when `thread_id` is absent. Null, blank, and non-string thread IDs are rejected. Downstream typed ports validate inputs.
3. Consul reads `workflow/local-agent/config` as JSON; another JSON field node extracts `/system_prompt`.
4. The model resource receives the parsed model ID. The agent receives the parsed message, thread ID, and Consul system prompt.
5. The agent resolves the model, an in-memory LangGraph checkpoint, and HTTP request tools, then runs LangChain `create_agent`.

The installed Docker model ID is `docker.io/ai/gemma4:e2b`. Its container-facing API is `http://host.docker.internal:12434/engines/v1`. Compose maps that hostname through Docker's host gateway. The local API placeholder is not a real credential.

## Invoke

Select **Incoming agent request**, then the **Live endpoint** panel. Copy its token into `WEBHOOK_TOKEN` in your shell, or use **Copy curl command** and replace the payload. Do not use the platform login token as the webhook token.

```sh
curl --fail-with-body -X POST 'http://127.0.0.1:8080/hooks/4eb73c2c-f4b0-41a8-931f-8bd35452fa70' \
  -H "Authorization: Bearer $WEBHOOK_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{
    "model_id": "docker.io/ai/gemma4:e2b",
    "thread_id": "local-verification-1",
    "message": "Use your HTTP request tool to GET /v1/kv/workflow/local-agent/tool-target?raw. What is the verification_code and status?"
  }'
```

The endpoint waits for the agent and returns HTTP 200 with its answer in `response`. The agent node’s **Webhook response output** is set to **Response** and published. If execution exceeds 50 seconds, HTTP 202 includes a `statusUrl`; GET it using the same bearer token to retrieve the answer. Add `?wait=0` to request immediate queuing.

## Consul

The local Consul UI is http://127.0.0.1:8500/ui/. Its data lives in the Compose `consul_data` volume. It is a single local server with a loopback-bound host port, not an authenticated production Consul cluster.

- System prompt: `workflow/local-agent/config`, JSON object with `system_prompt`.
- HTTP tool verification data: `workflow/local-agent/tool-target`.

Change the system prompt through Consul's KV editor; the next invocation reads it without a workflow edit.

## Plugin versions and boundaries

- `community.langchain-agent@1.2.0`: optional input ports override configured system prompt and thread ID; `tool_calls` output reports the number of tool calls.
- `community.openai-model@1.1.0`: optional model input overrides configured model ID.
- `community.in-memory-checkpoint@1.0.0`: checkpoint exists only within a single agent invocation and is discarded afterward, including when the next request has the same thread ID.
- `community.http-request-tools@1.3.0`: agent tool restricted to an explicit base URL and allowed HTTP methods. The `path` argument accepts full URLs allowed by the component settings, or relative paths against `base_url`. Runtime hostname restrictions, response size limits, timeouts, and redirect checks also apply.

Existing plugin versions remain available for pinned workflows. Resource resolution and HTTP calls retain SDK OpenTelemetry identity and tracing.

## Verified locally

The incoming curl POST produced run `570a65d9-45bc-4172-a686-f101848c3ac5`: all 11 steps succeeded. Docker Model Runner recorded the agent's actual `http_request` call, the tool response with HTTP 200, and the final answer: “The verification_code is `CONSUL-HTTP-4829` and the status is `operational`.”

A second POST with the same thread ID produced run `77dbccf4-1c4c-4809-aacf-ffcebce370b0`. It also succeeded; the model request contained only the new system/user messages and answered `fresh-run-ok`, confirming no cross-run history. Plugin manifest validation, `go build ./...`, and `go vet ./...` passed.

Local service setup follows the [Consul Docker deployment guide](https://developer.hashicorp.com/consul/docs/deploy/server/docker) and [Docker Model Runner API documentation](https://docs.docker.com/ai/model-runner/api-reference/).

Response delivery verified on published revision 9: incoming curl POST returned HTTP 200 with `response: "The verification_code is `CONSUL-HTTP-4829` and the status is `operational`."` (run `42782386-8f67-4ff3-a4e2-365be1d61eeb`). The authenticated status endpoint returned the same result; requests without the webhook token returned HTTP 401.

`thread_id` is optional as of published revision 10. `community.thread-id@1.0.0` generates an RFC 9562 UUIDv7 from a 48-bit Unix millisecond timestamp and cryptographically random bits. The existing successful-step cache preserves this output across workflow retry attempts. `model_id` and `message` remain required.

Published revision 11 enables `https://example.com` and `http://example.com` in the HTTP tool’s `allowed_origins`. The worker hostname allowlist also includes `example.com`. The Consul prompt describes the available HTTP capability; the tool description lists configured origins dynamically. Fetch another service by configuring both the plugin’s allowed origins and the deployment hostname allowlist.

External HTML retrieval verified: run `ca6da3f4-f698-4a15-87f5-1779004c23c1` returned HTTP 200 and summarized the actual Example Domain page through the agent HTTP tool. The SDK HTTP helper now removes transport encoding headers after consuming decoded bytes, preventing double decompression of gzip responses while preserving the decoded response size limit.
