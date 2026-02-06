# Skyline MCP Server (Node.js)

A Node.js MCP server that connects to [Skyline](https://github.com/your-org/skyline-mcp) API Gateway, providing centralized API access with profile-based authentication and operation filtering.

## Features

- 🚀 **Connects to Skyline Gateway** via WebSocket for bidirectional communication
- 🔐 **Profile-based authentication** with secure token validation
- 🛠️ **Dynamic tool loading** from Skyline profiles
- 🔄 **Automatic reconnection** with exponential backoff
- 📡 **Real-time notifications** support (foundation for streaming)
- 🎯 **MCP Protocol compliant** using official SDK

## Architecture

```
LLM ←stdio→ Skyline MCP Server ←WebSocket→ Skyline Gateway ←HTTP→ Real APIs
```

All API credentials and configurations are managed centrally in Skyline, keeping them secure and separate from the MCP server.

## Prerequisites

- Node.js 18+ or compatible runtime
- Running [Skyline](../../cmd/skyline-server) instance
- Profile configured in Skyline with API access

## Installation

### From Source

```bash
cd clients/node-mcp-server
npm install
npm run build
```

### Global Installation (Optional)

```bash
npm install -g .
```

## Configuration

The MCP server is configured via environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SKYLINE_URL` | No | `http://localhost:9190` | Skyline gateway base URL |
| `SKYLINE_PROFILE` | **Yes** | - | Profile name in Skyline |
| `SKYLINE_TOKEN` | **Yes** | - | Profile authentication token |

## Usage

### Standalone

```bash
export SKYLINE_URL="http://localhost:9190"
export SKYLINE_PROFILE="my-profile"
export SKYLINE_TOKEN="your-token-from-skyline-ui"

node dist/index.js
```

### With Claude Desktop

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "skyline": {
      "command": "node",
      "args": [
        "/path/to/skyline-mcp/clients/node-mcp-server/dist/index.js"
      ],
      "env": {
        "SKYLINE_URL": "http://localhost:9190",
        "SKYLINE_PROFILE": "my-profile",
        "SKYLINE_TOKEN": "your-token-from-skyline-ui"
      }
    }
  }
}
```

### With Cline VSCode Extension

Add to your Cline MCP settings:

```json
{
  "mcpServers": {
    "skyline": {
      "command": "node",
      "args": [
        "/path/to/skyline-mcp/clients/node-mcp-server/dist/index.js"
      ],
      "env": {
        "SKYLINE_URL": "http://localhost:9190",
        "SKYLINE_PROFILE": "my-profile",
        "SKYLINE_TOKEN": "your-token-from-skyline-ui"
      }
    }
  }
}
```

## Quick Start

### 1. Start Skyline Gateway

```bash
# Set encryption key
export CONFIG_SERVER_KEY="base64:$(openssl rand -base64 32)"

# Start Skyline
cd ../../cmd/skyline-server
go run . --listen :9190
```

### 2. Create Profile in Skyline

1. Open http://localhost:9190/ui/
2. Create a new profile (e.g., "dev")
3. Add your APIs (OpenAPI, GraphQL, gRPC, etc.)
4. Configure operation filters if needed
5. Copy the profile token

### 3. Run MCP Server

```bash
export SKYLINE_URL="http://localhost:9190"
export SKYLINE_PROFILE="dev"
export SKYLINE_TOKEN="<token-from-step-2>"

npm start
```

### 4. Test with MCP Inspector

```bash
npx @modelcontextprotocol/inspector node dist/index.js
```

## How It Works

### Connection Flow

1. **Startup**: MCP server connects to Skyline via WebSocket
   ```
   ws://localhost:9190/profiles/{profile}/gateway
   Authorization: Bearer <profile-token>
   ```

2. **Tool Discovery**: Fetches available tools from Skyline
   ```json
   {
     "jsonrpc": "2.0",
     "id": 1,
     "method": "tools/list"
   }
   ```

3. **Tool Execution**: Proxies tool calls to Skyline
   ```json
   {
     "jsonrpc": "2.0",
     "id": 2,
     "method": "execute",
     "params": {
       "tool_name": "petstore__getPetById",
       "arguments": { "petId": "123" }
     }
   }
   ```

4. **Real-time Updates**: Receives notifications from Skyline
   ```json
   {
     "jsonrpc": "2.0",
     "method": "notification",
     "params": { ... }
   }
   ```

### Security Model

- **No API Credentials in MCP Server**: All API keys, tokens, and secrets are stored securely in Skyline
- **Profile Token Authentication**: MCP server only needs a single profile token to access all APIs in that profile
- **Operation Filtering**: Skyline enforces allowlist/blocklist filters before execution
- **Centralized Audit**: All API calls are logged in Skyline gateway

## Development

### Build

```bash
npm run build
```

### Watch Mode

```bash
npm run dev
```

### Type Checking

```bash
npx tsc --noEmit
```

## Troubleshooting

### "Failed to connect to Skyline"

- Check that Skyline is running: `curl http://localhost:9190/healthz`
- Verify `SKYLINE_URL` is correct
- Check firewall/network settings

### "401 Unauthorized"

- Verify `SKYLINE_PROFILE` matches a profile in Skyline
- Check that `SKYLINE_TOKEN` is correct (copy from Skyline UI)
- Ensure profile hasn't been deleted or token rotated

### "Tool not found"

- Check that the API is configured in your Skyline profile
- Verify operation filters aren't blocking the operation
- Refresh tools: restart the MCP server

### WebSocket Disconnects

The MCP server automatically reconnects with exponential backoff:
- Attempt 1: 1 second delay
- Attempt 2: 2 seconds delay
- Attempt 3: 4 seconds delay
- Up to 5 attempts

## API Reference

### SkylineClient

```typescript
class SkylineClient extends EventEmitter {
  constructor(config: SkylineConfig)

  async connect(): Promise<void>
  disconnect(): void

  async fetchTools(): Promise<Tool[]>
  async executeTool(toolName: string, args: Record<string, any>): Promise<ExecutionResult>

  async subscribe(resource: string, params?: Record<string, any>): Promise<{ subscription_id: string }>
  async unsubscribe(subscriptionId: string): Promise<void>
}
```

### Events

- `connected`: WebSocket connection established
- `disconnected`: WebSocket connection closed
- `reconnecting`: Attempting to reconnect
- `error`: Error occurred
- `notification`: Server-initiated notification received

## License

MIT

## Related Projects

- [Skyline Gateway](../../cmd/skyline-server) - Centralized API gateway for MCP
- [Model Context Protocol](https://modelcontextprotocol.io/) - MCP specification
