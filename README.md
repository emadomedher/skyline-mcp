# mcp-api-bridge

![Go](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go&logoColor=white)
![MCP](https://img.shields.io/badge/MCP-stdio-000000)
![Status](https://img.shields.io/badge/status-active-success)
![License](https://img.shields.io/badge/license-TBD-lightgrey)

Production-grade MCP stdio server that dynamically exposes external APIs as MCP tools/resources.
It loads a YAML config at startup, fetches each API spec, auto-detects the spec type, normalizes it into a canonical
model, generates MCP tools/resources, and serves MCP JSON-RPC over stdin/stdout. Logs go to stderr only.

**Highlights**
- Auto-detect OpenAPI 3, Swagger 2, Google Discovery, WSDL, Jenkins
- Canonical model firewall (no direct spec → tool mapping)
- Safe auth handling with redaction and env expansion
- Deterministic tool naming and schemas

Secrets are never logged or emitted in MCP-visible schemas or responses.

## TL;DR

Run the mock APIs:

```bash
go run ./examples/mock
```

Start the config server (encrypted profiles):

```bash
export CONFIG_SERVER_KEY="base64:<32-byte-key>"
go run ./cmd/config-server --listen :9190 --storage ./profiles.enc.yaml --auth-mode bearer
```

Create a profile:

```bash
curl -X PUT http://localhost:9190/profiles/dev \
  -H "Authorization: Bearer dev-token" \
  -H "Content-Type: application/json" \
  -d '{"token":"dev-token","config_yaml":"apis:\n  - name: petstore-openapi\n    spec_url: http://localhost:9999/openapi/openapi.json\n    base_url_override: http://localhost:9999/openapi\n"}'
```

Run the MCP bridge using that profile (agent never sees creds):

```bash
export MCP_PROFILE=dev
export MCP_PROFILE_TOKEN=dev-token
go run ./cmd/mcp-api-bridge --config-url http://localhost:9190
```

## Supported API Types (Auto-Detected)

Implemented:
- OpenAPI 3.x (JSON/YAML)
- Swagger/OpenAPI 2.0 (JSON/YAML) via conversion to OpenAPI 3
- Google API Discovery docs (JSON)
- GraphQL SDL or GraphQL introspection JSON
- WSDL 1.1 (XML) for SOAP 1.1/1.2 bindings
- Jenkins object graph (JSON/XML, read-only traversal)

Planned (not implemented yet):
- gRPC
- Fallback REST (no spec)
- JSON-RPC upstream APIs

## Auto-Detection (No Type Required)

You only provide `spec_url`. At startup, each spec is fetched and checked by adapters in order:
- OpenAPI 3 adapter (looks for `openapi` key in JSON/YAML)
- Swagger 2 adapter (looks for `swagger: "2.0"` in JSON/YAML)
- Google Discovery adapter (looks for `kind: discovery#*`)
- GraphQL adapter (SDL or introspection JSON; SDL looks for `type Query`/`type Mutation` or a `schema` block)
- Jenkins adapter (looks for Jenkins `_class` in JSON or `<hudson>`/`<jenkins>` in XML)
- WSDL adapter (looks for `<definitions>`/`<wsdl:definitions>`)

If no adapter matches, startup fails for that API.

## What Is Supported Per Type

OpenAPI 3.x + Swagger 2.0 (after conversion):
- `servers[].url` base (OpenAPI) or `schemes`/`host`/`basePath` (Swagger 2)
- paths + HTTP methods
- `operationId` (falls back to normalized `method_path`)
- parameters in `path`, `query`, `header` (auth headers are excluded)
- JSON `requestBody` or Swagger 2 `in: body`
- response schema (best-effort; used for tool output schema only)

WSDL 1.1:
- First service/port/binding is used
- SOAPAction header is injected when present
- Inputs are modeled as `arguments.parameters` (key/value) and optional `arguments.body` (raw XML)
- SOAP responses are parsed into JSON when possible

Jenkins:
- Read-only graph traversal using `/api/json`
- Tools accept `tree`/`depth` query parameters to limit payload size
- `getObject` takes a Jenkins object URL/path and appends `/api/json` if missing
- Write operations are not auto-discovered; explicit allowlisting is required
- Crumb support is automatic for allowlisted writes (`/crumbIssuer/api/json`)

Google API Discovery:
- Resources + methods are converted to tools
- Parameters in `path`, `query`, `header` are supported
- Request/response schemas use Discovery `schemas` (best-effort)

GraphQL (SDL or introspection):
- Query + Mutation fields are converted to tools
- Arguments are passed as variables
- Object return types default to a safe scalar selection; override with `selection`
- `base_url_override` is required (SDL/introspection do not include the HTTP endpoint)
- If `spec_url` points at a GraphQL endpoint (e.g., `/api/graphql`), the bridge will POST an introspection query to fetch the schema

Jira Cloud (REST via OpenAPI):
- Use the Atlassian OpenAPI spec: `https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json`
- Set `base_url_override` to your Jira Cloud base URL (e.g., `https://webookcom.atlassian.net`)

## Configuration

Example config: `examples/config.yaml.example` (copy to `examples/config.yaml`)

```yaml
apis:
  - name: petstore-openapi
    spec_url: http://localhost:9999/openapi/openapi.json
    base_url_override: http://localhost:9999/openapi
    auth:
      type: bearer
      token: ${PETSTORE_OPENAPI_TOKEN}
  - name: local-api
    spec_file: /path/to/local/openapi.json
    base_url_override: https://api.example.com
    auth:
      type: api-key
      header: X-API-Key
      value: ${API_KEY}
  - name: plantsStore-wsdl
    spec_url: http://localhost:9999/wdsl/wsdl
    auth:
      type: bearer
      token: ${PLANTS_WSDL_TOKEN}
  - name: dinosaurs-swagger2
    spec_url: http://localhost:9999/swagger/swagger.json
    auth:
      type: bearer
      token: ${DINOSAURS_SWAGGER2_TOKEN}
  - name: cars-graphql
    spec_url: http://localhost:9999/graphql/schema
    base_url_override: http://localhost:9999/graphql
    auth:
      type: basic
      username: ${GRAPHQL_USERNAME}
      password: ${GRAPHQL_PASSWORD}
  - name: webook-dot-rocks-jenkins
    spec_url: https://cicd.webook.rocks/api/json
    base_url_override: https://cicd.webook.rocks
    auth:
      type: basic
      username: ${JENKINS_USERNAME}
      password: ${JENKINS_PASSWORD}
    jenkins:
      allow_writes:
        - name: triggerJob
          method: POST
          path: /job/{job}/build
          summary: Trigger a Jenkins job build.
        - name: triggerJobWithParameters
          method: POST
          path: /job/{job}/buildWithParameters
          summary: Trigger a Jenkins job build with query parameters.
```

## Config Server (Profiles + Encrypted Storage)

For multi-agent setups, run the standalone config server. It stores **profiles** (each profile = one MCP config YAML)
in an encrypted file on disk and serves them to the MCP server. Agents never receive credentials directly.

### Start the config server

```bash
export CONFIG_SERVER_KEY="base64:<32-byte-key>"
go run ./cmd/config-server --listen :9190 --storage ./profiles.enc.yaml --auth-mode bearer
```

`CONFIG_SERVER_KEY` can be:
- 32 raw bytes
- base64-encoded 32 bytes (prefix `base64:` optional)
- hex-encoded 32 bytes (prefix `hex:` optional)

### Create or update a profile

```bash
curl -X PUT http://localhost:9190/profiles/dev \
  -H "Authorization: Bearer dev-token" \
  -H "Content-Type: application/json" \
  -d '{
    "token": "dev-token",
    "config_yaml": "apis:\n  - name: petstore-openapi\n    spec_url: http://localhost:9999/openapi/openapi.json\n    base_url_override: http://localhost:9999/openapi\n"
  }'
```

### Web UI

Open `http://localhost:9190/ui/` after starting the config server.

### Use a profile from MCP

```bash
export MCP_PROFILE=dev
export MCP_PROFILE_TOKEN=dev-token
go run ./cmd/mcp-api-bridge --config-url http://localhost:9190
```

Auth modes:
- `none` (no auth)
- `bearer` (per-profile bearer token)

### Spec Sources

Each API must specify either `spec_url` (for remote specs) or `spec_file` (for local files):

- **`spec_url`**: HTTP(S) URL to fetch the API spec from. Supports environment variable expansion (e.g., `${BASE_URL}/openapi.json`).
- **`spec_file`**: Local file path to the API spec. Supports environment variable expansion (e.g., `${HOME}/specs/api.json`). Useful for:
  - Generated specs from tools like Oracle Cloud OpenAPI generator
  - Cached/offline API specs
  - Custom or modified spec files
  - Development and testing scenarios

**Note**: `spec_url` and `spec_file` are mutually exclusive - you must specify exactly one per API.

Auth types (used for API calls and spec fetches):
- `bearer` (token)
- `basic` (username/password)
- `api-key` (header/value)

SSE auth is configured via flags (`--sse-auth-*`) and supports the same types.

Global + per-API options:
- `timeout_seconds` (default 10)
- `retries` (default 0)

Jenkins write allowlist:
- `jenkins.allow_writes[]` defines explicit write tools (no inference).
- Each entry requires `name`, `method`, `path`, optional `summary`.
- Query params for `buildWithParameters` are passed via `arguments.parameters` (object).

## Tool Naming

Tool names are stable and deterministic:
- `{api_name}__{operationId}` (double underscore separator)
- If `operationId` is missing: `{api_name}__{method}_{path}` normalized

## Tool Inputs

OpenAPI/Swagger tools accept:
- Path/query/header parameters as named fields
- JSON request bodies via `body`

WSDL/SOAP tools accept:
- `parameters` (object) -> server builds SOAP XML automatically
- `body` (string) optional, raw SOAP XML if you already have it

Jenkins tools accept:
- `tree` (string) and `depth` (integer) to limit the returned graph
- `url` for `getObject` (object URL or path); `/api/json` is appended if missing
- `parameters` (object) for write allowlist query parameters (e.g. `buildWithParameters`)

Tool descriptions include parameter names, types, and required/optional hints to guide LLM usage.

## Responses and Normalization

All tool calls return `content` as text containing JSON. The JSON payload has:
- `status` (HTTP status code)
- `content_type`
- `body` (parsed JSON if possible, otherwise string)

SOAP XML responses are parsed to JSON when possible to avoid clogging context with raw XML.

## Security

- Secrets can reference `${ENV_VAR}`.
- Secrets are never logged and are redacted from errors.
- Auth headers are not exposed in schemas or responses.

## Local Demo

Copy the example config and set tokens:

```bash
cp examples/config.yaml.example examples/config.yaml

export PETSTORE_OPENAPI_TOKEN=demo-token
export PLANTS_WSDL_TOKEN=mock-token
export DINOSAURS_SWAGGER2_TOKEN=dino-token
export GRAPHQL_USERNAME=graphql-user
export GRAPHQL_PASSWORD=graphql-pass
export JENKINS_USERNAME=demo-user
export JENKINS_PASSWORD=demo-pass
```

Run the mock API:

```bash
go run ./examples/mock
```

This also starts the gRPC clothes mocks (unary for now).
Default ports: hats `:50051`, shoes `:50052`, pants `:50053`, shirts `:50054`.

Run the MCP server:

```bash
go run ./cmd/mcp-api-bridge --config ./examples/config.yaml
```

## MCP JSON-RPC Examples

Initialize:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
```

List tools:

```json
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
```

List pets (OpenAPI):

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"petstore-openapi__listPets","arguments":{"limit":2}}}
```

List plants (WSDL/SOAP):

```json
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"plantsStore-wsdl__ListPlants","arguments":{}}}
```

List dinosaurs (Swagger 2.0):

```json
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"dinosaurs-swagger2__listDinosaurs","arguments":{"limit":5}}}
```

List cars (GraphQL):

```json
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"cars-graphql__query_listCars","arguments":{"limit":2}}}
```

Get Jenkins root (read-only):

```json
{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"webook-dot-rocks-jenkins__getRoot","arguments":{"tree":"jobs[name,url]"}}}
```

Trigger a Jenkins job (write allowlist):

```json
{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"webook-dot-rocks-jenkins__triggerJob","arguments":{"job":"example-job"}}}
```

Trigger a Jenkins job with parameters:

```json
{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"webook-dot-rocks-jenkins__triggerJobWithParameters","arguments":{"job":"example-job","parameters":{"branch":"main","env":"staging"}}}}
```

## SSE Mode (HTTP)

Start the server in SSE mode with auth:

```bash
go run ./cmd/mcp-api-bridge --config ./examples/config.yaml \
  --transport sse --listen :8080 \
  --sse-auth-type bearer --sse-auth-token dev-token
```

Connect to SSE and send a request:

```bash
# Open SSE stream (copy the message URL from the first "endpoint" event)
curl -N -H "Authorization: Bearer dev-token" http://localhost:8080/sse

# Post a JSON-RPC request to the message URL returned by /sse
curl -H "Authorization: Bearer dev-token" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  'http://localhost:8080/message?session_id=...'
```

## Streamable HTTP Mode (MCP)

Start the server in streamable HTTP mode:

```bash
go run ./cmd/mcp-api-bridge --config ./examples/config.yaml \
  --transport http --listen :8080
```

`/mcp` expects JSON-RPC over HTTP. For now it supports POST responses (no SSE streaming yet).
Requests must include:
- `Content-Type: application/json`
- `Accept: application/json, text/event-stream`
- Optional `Mcp-Protocol-Version` (accepted: `2025-03-26`, `2025-06-18`, `2025-11-25`)
- Auth headers if configured (same as SSE)

Send a request to `/mcp`:

```bash
curl -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-03-26" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  'http://localhost:8080/mcp'
```

## Troubleshooting

- Spec fetch failures: ensure the mock server is running and URLs are reachable.
- 401 from mocks: make sure the token env vars match in both the mock server and MCP server.
- Tool not found: re-run the MCP server after changing config or spec URLs.

## Tests

```bash
go test ./...
```

## Build

```bash
mkdir -p bin
go build -o ./bin/mcp-api-bridge ./cmd/mcp-api-bridge
```

## Project Layout

- `cmd/mcp-api-bridge` — main MCP server (stdio + streamable HTTP)
- `cmd/config-server` — profile/config server (encrypted storage + web UI)
- `internal/config` — config models, env expansion, remote profile fetch
- `internal/spec` — spec fetching, adapter detection, GraphQL introspection
- `internal/openapi` — OpenAPI -> canonical parsing (sanitizes bad examples)
- `internal/swagger2` — Swagger 2.0 -> OpenAPI 3 conversion
- `internal/wsdl` — WSDL -> canonical parsing
- `internal/jenkins` — Jenkins object graph adapter
- `internal/googleapi` — Google API Discovery adapter
- `internal/graphql` — GraphQL SDL/introspection adapter
- `internal/canonical` — normalized models
- `internal/mcp` — MCP server + registry
- `internal/runtime` — HTTP executor + auth injection
- `internal/redact` — log redaction helpers
- `examples/config.yaml.example` — sample config (copy to `examples/config.yaml`)
- `examples/config.mock.yaml` — local mock-only config
- `examples/mock` — local mock API server (OpenAPI + Swagger2 + WSDL + GraphQL + gRPC)
- `examples/mock/clothes.proto` — gRPC clothes proto (unary)
- `examples/config-server.env.example` — config server env template

## TL;DR
The project is at a very early stage, It is definitely not ready for production use. But PoC is done and it performs great
