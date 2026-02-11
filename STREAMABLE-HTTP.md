# Skyline MCP - Streamable HTTP Transport

**Status:** ✅ **Implemented** (MCP Spec 2025-11-25 compliant)

## Overview

Skyline now supports **Streamable HTTP**, the official MCP transport specified in the 2025-11-25 protocol version. This transport unifies client requests and server notifications under a single `/mcp` endpoint.

### What is Streamable HTTP?

Streamable HTTP is the current standard MCP transport that:
- Uses a single `/mcp` endpoint for all communication
- Supports POST for client requests (with JSON or SSE responses)
- Supports GET for server notifications (SSE stream)
- Supports DELETE for explicit session termination
- Includes session management via `Mcp-Session-Id` header
- Provides resumability via SSE event IDs

### Advantages Over HTTP+SSE (Deprecated)

| Feature | HTTP+SSE (old) | Streamable HTTP (new) |
|---------|----------------|----------------------|
| Endpoints | `/sse` + `/message` | Single `/mcp` |
| Session ID | Query param | Header |
| Auth | Checked once | Every request |
| CORS | Custom handling | Standard HTTP |
| Load balancing | Sticky sessions | Stateless |
| Spec version | 2024-11-05 | 2025-11-25 |
| Status | Deprecated | Current standard |

## Usage

### Starting the Server

```bash
# Streamable HTTP (recommended)
./skyline-server --config config.yaml --transport http --listen :8080

# With authentication
./skyline-server --config config.yaml --transport http --listen :8080 \
  --sse-auth-type bearer --sse-auth-token "your-secret-token"

# Legacy HTTP+SSE (backwards compatibility)
./skyline-server --config config.yaml --transport sse --listen :8080
```

### Transport Options

- `http` or `streamable-http` - **Recommended** (MCP spec 2025-11-25)
- `sse` - Legacy HTTP+SSE (backwards compatibility only)
- `stdio` - Standard input/output (for local clients)

## Client Integration

### 1. Initialize Session

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-11-25" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-11-25",
      "capabilities": {},
      "clientInfo": {
        "name": "my-client",
        "version": "1.0.0"
      }
    }
  }'
```

**Response:**
- Status: `200 OK`
- Header: `Mcp-Session-Id: <session-id>`
- Body: InitializeResult (JSON)

**Save the `Mcp-Session-Id` header value** - you'll need it for all subsequent requests.

### 2. Make Requests

```bash
SESSION_ID="<session-id-from-initialize>"

# List tools
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list"
  }'

# Call a tool
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "graphql_gitlab_getProject",
      "arguments": {
        "fullPath": "my-group/my-project"
      }
    }
  }'
```

### 3. Open Notification Stream (Optional)

```bash
# Opens an SSE stream for server-to-client notifications
curl -N http://localhost:8080/mcp \
  -H "Accept: text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID"
```

Server can send notifications like:
- `notifications/tools/list_changed` - Tool list updated
- `notifications/resources/list_changed` - Resource list updated
- Custom server events

### 4. Batch Requests

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '[
    {"jsonrpc": "2.0", "id": 10, "method": "tools/list"},
    {"jsonrpc": "2.0", "id": 11, "method": "resources/list"}
  ]'
```

### 5. Terminate Session

```bash
curl -X DELETE http://localhost:8080/mcp \
  -H "Mcp-Session-Id: $SESSION_ID"
```

Response: `204 No Content`

## Authentication

All three auth types work with Streamable HTTP:

### Bearer Token

```bash
./skyline-server --transport http --listen :8080 \
  --sse-auth-type bearer --sse-auth-token "your-secret-token"

# Client:
curl -H "Authorization: Bearer your-secret-token" ...
```

### Basic Auth

```bash
./skyline-server --transport http --listen :8080 \
  --sse-auth-type basic --sse-auth-username admin --sse-auth-password secret

# Client:
curl -u admin:secret ...
```

### API Key

```bash
./skyline-server --transport http --listen :8080 \
  --sse-auth-type api-key --sse-auth-header X-API-Key --sse-auth-value "your-key"

# Client:
curl -H "X-API-Key: your-key" ...
```

## Resumability (Advanced)

The server assigns SSE event IDs for resumability. If a GET stream disconnects, clients can resume:

```bash
# Initial connection
curl -N http://localhost:8080/mcp \
  -H "Accept: text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID"

# Output:
# id: session-abc-1
# data: {"jsonrpc":"2.0","method":"notifications/tools/list_changed"}
#
# id: session-abc-2
# data: {"jsonrpc":"2.0","method":"notifications/resources/list_changed"}

# Connection drops...

# Resume from last event
curl -N http://localhost:8080/mcp \
  -H "Accept: text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -H "Last-Event-ID: session-abc-2"

# Server replays events after session-abc-2
```

The server keeps the last 100 events per session for resumability.

## Session Management

### Session Lifecycle

1. **Create:** POST /mcp with `initialize` method
2. **Use:** Include `Mcp-Session-Id` header in all requests
3. **Expire:** Automatic after 1 hour of inactivity
4. **Terminate:** DELETE /mcp (explicit cleanup)

### Session Cleanup

- Inactive sessions (>1 hour) are automatically cleaned every 5 minutes
- Closed GET streams don't terminate the session (allows reconnection)
- Explicit DELETE terminates immediately

## Testing

### Quick Test with curl

```bash
# 1. Initialize
RESP=$(curl -sS -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Mcp-Protocol-Version: 2025-11-25" \
  -D - \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}')

SESSION_ID=$(echo "$RESP" | grep -i "Mcp-Session-Id:" | cut -d' ' -f2 | tr -d '\r')

echo "Session ID: $SESSION_ID"

# 2. List tools
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | jq '.result.tools | length'

# 3. Terminate
curl -X DELETE http://localhost:8080/mcp -H "Mcp-Session-Id: $SESSION_ID"
```

### Automated Test Client

```bash
cd ~/code/skyline-mcp
go run test-streamable-http.go
```

Expected output:
```
=== Testing Skyline MCP Streamable HTTP ===

1. Initializing session (POST /mcp)...
   ✅ Session created: Kx9mP3wQz2Y1bN5r

2. Listing tools (POST /mcp)...
   ✅ Found 23 tools
      First tool: graphql_gitlab_getProject

3. Opening notification stream (GET /mcp)...
   ✅ Notification stream opened

4. Sending notification (POST /mcp)...
   ✅ Notification sent (HTTP 202)

5. Testing batch request (POST /mcp)...
   ✅ Batch completed: 2 responses

6. Terminating session (DELETE /mcp)...
   ✅ Session terminated

=== All tests completed! ===
```

## Security

### Built-in Protections

1. **Origin validation** - Prevents DNS rebinding attacks
2. **Auth on every request** - Not just at connection time
3. **Request size limits** - 10MB max per request
4. **Session timeout** - 1 hour inactivity
5. **Protocol validation** - Checks `Mcp-Protocol-Version` header
6. **CORS support** - Proper preflight handling

### Recommended Security

1. **Use HTTPS** - Run behind reverse proxy with TLS
2. **Enable auth** - Bearer token or API key
3. **Rate limiting** - Use reverse proxy (Traefik, nginx)
4. **Firewall** - Restrict access to trusted IPs

Example Traefik config:
```yaml
http:
  routers:
    skyline:
      rule: "Host(`api.example.com`) && PathPrefix(`/mcp`)"
      service: skyline
      tls:
        certResolver: letsencrypt
      middlewares:
        - auth
        - ratelimit

  middlewares:
    auth:
      basicAuth:
        users:
          - "admin:$apr1$..."
    ratelimit:
      rateLimit:
        average: 100
        period: 1m

  services:
    skyline:
      loadBalancer:
        servers:
          - url: "http://localhost:8080"
```

## Compatibility

### Works With

- ✅ Claude Desktop (Anthropic)
- ✅ OpenAI Codex IDE extension
- ✅ ChatGPT Desktop
- ✅ Any MCP client supporting Streamable HTTP (spec 2025-11-25)
- ✅ Official MCP SDKs (TypeScript, Python, Go, Swift)

### Protocol Versions Supported

- `2025-11-25` ✅ (latest)
- `2025-06-18` ✅
- `2025-03-26` ✅
- `2024-11-05` ⚠️ (HTTP+SSE only, use `--transport sse`)

## Migration from HTTP+SSE

If you're currently using `--transport sse`, migration is straightforward:

**Old (HTTP+SSE):**
```bash
./skyline-server --transport sse --listen :8080
```

**New (Streamable HTTP):**
```bash
./skyline-server --transport http --listen :8080
```

**Client changes:**
- Change endpoint from `/sse` + `/message` → `/mcp`
- Use `Mcp-Session-Id` header instead of `?session_id=` query param
- No other changes needed!

Both transports can run simultaneously during migration (different ports).

## Troubleshooting

### "session not found" on GET /mcp

**Cause:** Trying to open notification stream before initializing session.

**Fix:** Send `POST /mcp` with `initialize` method first.

### "missing Mcp-Session-Id header"

**Cause:** Forgot to include session ID in request header.

**Fix:** Add `-H "Mcp-Session-Id: <your-session-id>"` to curl command.

### "unauthorized"

**Cause:** Authentication required but not provided.

**Fix:** Add auth header (e.g., `-H "Authorization: Bearer <token>"`)

### Session expires quickly

**Cause:** Default timeout is 1 hour of inactivity.

**Fix:** Keep session alive by making requests or opening GET stream.

## Implementation Details

### File Structure

- `internal/mcp/streamable_http.go` - Streamable HTTP implementation (new)
- `internal/mcp/http_sse.go` - Legacy HTTP+SSE (backwards compat)
- `internal/mcp/server.go` - Core MCP server logic
- `cmd/mcp-api-bridge/main.go` - Transport selection

### Session Storage

- In-memory session store (thread-safe)
- Ring buffer for last 100 events per session
- Automatic cleanup every 5 minutes
- Channel-based SSE event delivery

### Event Flow

```
Client POST /mcp (initialize)
    ↓
Server creates session → returns Mcp-Session-Id header
    ↓
Client GET /mcp (with session ID)
    ↓
Server opens SSE stream → sends events to channel
    ↓
Client POST /mcp (with session ID)
    ↓
Server processes request → returns JSON or streams SSE
    ↓
Server can send notifications via session channel
    ↓
Client DELETE /mcp (with session ID)
    ↓
Server terminates session → closes channel
```

## References

- [MCP Specification 2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25)
- [MCP Transports](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)
- [Anthropic MCP Announcement](https://www.anthropic.com/news/model-context-protocol)
- [OpenAI Codex MCP Docs](https://developers.openai.com/codex/mcp)

---

**Status:** Production ready ✅  
**Spec compliance:** MCP 2025-11-25 ✅  
**Tested with:** curl, custom Go client ✅  
**Security:** Origin validation, auth, rate limiting recommended ✅
