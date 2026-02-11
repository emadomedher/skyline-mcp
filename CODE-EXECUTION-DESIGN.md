# Code Execution Design for Skyline MCP

## Overview
Add code execution capabilities to Skyline to enable 98% context reduction while maintaining full API functionality.

## Architecture

```
┌─────────────┐
│   LLM       │ sends code
└──────┬──────┘
       │ POST /execute
       ↓
┌────────────────────────────────┐
│   Skyline Server               │
│   ┌──────────────────────────┐ │
│   │  /execute endpoint       │ │
│   │  - Validates code        │ │
│   │  - Runs in Deno sandbox  │ │
│   └──────┬───────────────────┘ │
│          │                     │
│   ┌──────▼───────────────────┐ │
│   │  Code Generator          │ │
│   │  - MCP tools → TS files  │ │
│   └──────┬───────────────────┘ │
│          │                     │
│   ┌──────▼───────────────────┐ │
│   │  MCP Tool Handler        │ │
│   │  - Internal tool calls   │ │
│   └──────┬───────────────────┘ │
└──────────│──────────────────────┘
           │ HTTP to APIs
           ↓
    ┌──────────────┐
    │  Nextcloud   │
    │  GitLab      │
    │  etc.        │
    └──────────────┘
```

## API Specification

### POST /execute

**Request:**
```json
{
  "code": "import * as nc from './mcp/nextcloud';\nconst files = await nc.search({name: 'test'});\nconsole.log(files.length);",
  "language": "typescript",
  "timeout": 30
}
```

**Response:**
```json
{
  "stdout": "5\n",
  "stderr": "",
  "exitCode": 0,
  "executionTime": 1.234,
  "toolsCalled": ["nextcloud__search"]
}
```

## Code Generation

### From MCP Tools to TypeScript

**MCP Tool:**
```json
{
  "name": "nextcloud__files_sharing-shareapi-get-shares",
  "description": "Get file shares",
  "inputSchema": {
    "type": "object",
    "properties": {
      "OCS-APIRequest": {"type": "string", "default": "true"},
      "path": {"type": "string"}
    }
  }
}
```

**Generated TypeScript:**
```typescript
// ./mcp/nextcloud/getShares.ts
import { callMCPTool } from '../client.ts';

export interface GetSharesInput {
  'OCS-APIRequest'?: string;
  path?: string;
}

/** Get file shares */
export async function getShares(input?: GetSharesInput): Promise<any> {
  return callMCPTool('nextcloud__files_sharing-shareapi-get-shares', {
    'OCS-APIRequest': 'true',
    ...input
  });
}
```

### Filesystem Structure

```
workspace/
├── mcp/
│   ├── client.ts           # MCP tool caller
│   ├── nextcloud/
│   │   ├── index.ts        # Re-exports all tools
│   │   ├── getShares.ts
│   │   ├── createShare.ts
│   │   └── ... (135 more)
│   ├── gitlab/
│   │   └── ...
│   └── README.md
└── user_code.ts            # LLM's code runs here
```

## Security

### Deno Sandbox
- No network access (except to internal MCP handler)
- No filesystem access (except /workspace)
- CPU/memory limits
- 30s timeout default

### Allowed Imports
```typescript
// ✅ Allowed
import * as nextcloud from './mcp/nextcloud';
import * as gitlab from './mcp/gitlab';

// ❌ Blocked
import { exec } from 'child_process';
import * as https from 'https';
```

## Implementation Steps

### 1. Code Generator (Go)
- `internal/codegen/typescript.go` - Generate TS files from tools
- `internal/codegen/generator.go` - Main generator logic

### 2. Execution Engine (Go + Deno)
- `internal/executor/deno.go` - Deno subprocess wrapper
- `internal/executor/sandbox.go` - Security constraints

### 3. HTTP Handler (Go)
- `internal/mcp/execute.go` - /execute endpoint
- `internal/mcp/streamable_http.go` - Add route

### 4. MCP Client (TypeScript/Deno)
- `runtime/mcp-client.ts` - Tool caller for Deno
- Generated per API on demand

## Testing Strategy

### Unit Tests
```go
func TestCodeGeneration(t *testing.T) {
    // Test: MCP tool → TypeScript generation
}

func TestSandboxSecurity(t *testing.T) {
    // Test: Network/filesystem blocking
}
```

### Integration Tests
```typescript
// Test: End-to-end code execution
const result = await fetch('http://localhost:8080/execute', {
  method: 'POST',
  body: JSON.stringify({
    code: `
      import * as nc from './mcp/nextcloud';
      const files = await nc.search({ name: 'test' });
      console.log(files.length);
    `
  })
});
```

### Performance Tests
- Measure context reduction (target: 95%+)
- Measure execution overhead (target: <500ms)

## Rollout Plan

### Stage 1: Prototype (Today)
- ✅ Basic /execute endpoint
- ✅ TypeScript code generation
- ✅ Deno sandbox
- ✅ Test with Nextcloud

### Stage 2: Enhancement (This Week)
- Python support
- Better error messages
- Streaming logs
- Tool usage analytics

### Stage 3: Production (Next Week)
- Rate limiting
- Multi-tenancy
- Monitoring/metrics
- Documentation

## Success Metrics

- **Context Reduction:** 95%+ (from ~20KB to ~1KB per request)
- **Cost Reduction:** 98%+ (Opus → Haiku for routing)
- **Execution Time:** <1s for simple operations
- **API Coverage:** 100% of tools accessible via code

## Open Questions

1. **Caching:** Should we cache generated TypeScript files?
2. **Versioning:** How to handle API schema updates?
3. **Multi-API:** How to handle cross-API workflows?
4. **Observability:** What metrics matter most?

## References

- [Anthropic Blog: Code Execution with MCP](https://www.anthropic.com/engineering/code-execution-with-mcp)
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-11-25)
- [Deno Security](https://deno.land/manual/getting_started/permissions)
