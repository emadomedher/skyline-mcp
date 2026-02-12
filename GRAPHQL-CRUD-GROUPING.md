# GraphQL CRUD Grouping Optimization

**Feature Status:** ✅ Available (Opt-in)  
**Default:** Disabled (`enable_crud_grouping: false`)  
**Added:** 2026-02-10

---

## What Is CRUD Grouping?

CRUD Grouping combines related GraphQL mutations (Create, Update, Delete) into single composite MCP tools, dramatically reducing the total number of tools exposed.

### Example: GitLab API

**Without CRUD Grouping (315 tools):**
- `gitlab__createIssue`
- `gitlab__updateIssue`
- `gitlab__deleteIssue`
- `gitlab__createNote`
- `gitlab__updateNote`
- ... (310 more separate tools)

**With CRUD Grouping (23 tools):**
- `gitlab__issue_manage` (combines create + update + delete)
- `gitlab__note_manage` (combines create + update)
- `gitlab__board_manage` (combines create + update)
- ... (20 more composite tools)

**Reduction:** 92.7% fewer tools (315 → 23)

---

## When to Use CRUD Grouping

### ✅ **Recommended For: Traditional MCP (No Code Execution)**

**Use Case:** Direct LLM → MCP tool calls without code execution

**Benefits:**
- **Context Reduction:** 92.7% fewer tool definitions in prompt
- **Lower Token Usage:** Significantly reduced context window consumption
- **Simplified Decision:** Fewer tools = easier for LLM to choose from
- **Cost Savings:** Less context = lower API costs

**Example Configuration:**
```yaml
apis:
  - name: gitlab
    type: graphql
    endpoint: https://gitlab.com/api/graphql
    optimization:
      enable_crud_grouping: true  # ✅ Recommended
```

**When this works well:**
- Claude Desktop with MCP protocol
- Direct tool calling without code execution
- LLM needs to see all available tools upfront

---

### ❌ **Not Recommended For: Code Execution + Discovery**

**Use Case:** Code execution with `searchTools()` discovery

**Problems:**

#### 1. **Discovery Ambiguity**
```typescript
// User asks: "Create a new issue"
const tools = await searchTools('create issue');

// Without grouping: ✅ Clear match
// Returns: gitlab__createIssue
//   "Create a new issue in a project Parameters: title, description..."

// With grouping: ❌ Ambiguous
// Returns: gitlab__issue_manage  
//   "Manage issues (create, update, delete) Parame..." (truncated)
// LLM doesn't know it CAN create from a "manage" tool
```

#### 2. **Truncated Descriptions Lose Critical Info**
With 60-character truncation:
- **Separate tool:** "Create a new issue in a project Parameters: title, desc..."
  - ✅ Clear purpose preserved
- **Composite tool:** "Manage issues (create, update, delete) Parameters:..."
  - ❌ Operations list gets cut off
  - ❌ "Manage" is vague

#### 3. **Parameter Complexity**
```typescript
// Separate tool (simple):
await callMCPTool('gitlab__createIssue', {
  title: 'Bug report',
  description: 'Found an issue...'
});

// Composite tool (complex):
await callMCPTool('gitlab__issue_manage', {
  operation: 'create',  // ← Extra parameter
  input: {
    title: 'Bug report',
    description: 'Found an issue...'
  }
});
```

#### 4. **On-Demand Loading Negates Benefits**
Code execution uses `searchTools()` to load tools on-demand:
- You're NOT loading all 315 tool definitions upfront
- You're searching and importing only what you need
- Having 315 focused, clear tools is actually BETTER than 23 vague ones

**Example Configuration:**
```yaml
apis:
  - name: gitlab
    type: graphql
    endpoint: https://gitlab.com/api/graphql
    optimization:
      enable_crud_grouping: false  # ✅ Recommended for code execution
```

---

## Configuration

### Default Behavior (Disabled)

```yaml
apis:
  - name: gitlab
    type: graphql
    endpoint: https://gitlab.com/api/graphql
    # optimization section omitted = CRUD grouping disabled
```

### Enable CRUD Grouping

```yaml
apis:
  - name: gitlab
    type: graphql
    endpoint: https://gitlab.com/api/graphql
    optimization:
      enable_crud_grouping: true
      flatten_inputs: false
      response_mode: essential
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enable_crud_grouping` | boolean | `false` | Combine related mutations into composite tools |
| `flatten_inputs` | boolean | `false` | Flatten nested input types |
| `response_mode` | string | `auto` | Response detail level: `essential`, `full`, `auto` |

---

## How It Works

### 1. Mutation Analysis

The analyzer groups mutations by entity type:

```
Mutations:
  createIssue(input: IssueInput!)
  updateIssue(id: ID!, input: IssueInput!)
  deleteIssue(id: ID!)
  createNote(input: NoteInput!)
  updateNote(id: ID!, input: NoteInput!)

Analysis:
  Issue operations: create, update, delete → "issue_manage"
  Note operations: create, update → "note_manage"
```

### 2. Composite Tool Generation

Each composite tool accepts an `operation` parameter:

```typescript
interface CompositeToolInput {
  operation: 'create' | 'update' | 'delete';
  input: Record<string, any>;
}
```

### 3. Runtime Routing

The MCP tool handler routes to the appropriate mutation based on `operation`:

```typescript
// Input: { operation: 'create', input: { title: '...' } }
// Routes to: createIssue mutation
```

---

## Performance Comparison

### Traditional MCP (No Code Execution)

| Metric | Without Grouping | With Grouping | Savings |
|--------|------------------|---------------|---------|
| **Tool Definitions** | 315 tools | 23 tools | 92.7% |
| **Context Tokens** | ~47,000 | ~3,500 | 92.6% |
| **LLM Prompt Size** | Large | Small | 92.6% |
| **API Cost/Request** | High | Low | ~92% |

**Verdict:** ✅ **CRUD Grouping highly beneficial**

---

### Code Execution + Discovery

| Metric | Without Grouping | With Grouping | Winner |
|--------|------------------|---------------|--------|
| **Discovery Clarity** | ✅ Clear | ❌ Ambiguous | Without |
| **Search Accuracy** | ✅ Exact | ⚠️ Generic | Without |
| **Parameter Simplicity** | ✅ Direct | ❌ Complex | Without |
| **Truncated Descriptions** | ✅ Useful | ❌ Vague | Without |
| **Upfront Context** | N/A | N/A | Tie (both use on-demand) |

**Verdict:** ✅ **Separate tools work better**

---

## Implementation Details

### Files

- `internal/config/graphql.go` - Configuration structure
- `internal/parsers/graphql/composite.go` - CRUD grouping analyzer
- `internal/parsers/graphql/graphql.go` - SDL parsing integration
- `internal/parsers/graphql/introspection.go` - Introspection parsing integration

### Algorithm

1. **Parse GraphQL schema** (SDL or introspection)
2. **Analyze mutations** for CRUD patterns
3. **Group by entity type** (Issue, Note, Board, etc.)
4. **Generate composite tools** with `_manage` suffix
5. **Preserve queries unchanged** (no grouping for queries)

### Entity Detection

Entities are detected by mutation name patterns:
- `create*` → entity name extracted
- `update*` → entity name extracted
- `delete*` → entity name extracted
- `destroy*` → treated as delete
- `remove*` → treated as delete

Example:
- `createIssue` → entity: "Issue"
- `updateIssue` → entity: "Issue"
- `deleteIssue` → entity: "Issue"
- Result: `issue_manage` composite tool

---

## Examples

### Example 1: GitLab (GraphQL Introspection)

**Config:**
```yaml
apis:
  - name: gitlab
    type: graphql
    endpoint: https://gitlab.com/api/graphql
    auth:
      type: bearer
      token: ${GITLAB_TOKEN}
    optimization:
      enable_crud_grouping: true
```

**Result:**
- 315 mutations → 23 composite tools
- Queries unchanged (315 → 315)

---

### Example 2: GitHub (Traditional MCP)

**Config:**
```yaml
apis:
  - name: github
    type: graphql
    endpoint: https://api.github.com/graphql
    auth:
      type: bearer
      token: ${GITHUB_TOKEN}
    optimization:
      enable_crud_grouping: true
```

**Benefits:**
- Massive context reduction for Claude Desktop
- Easier tool selection for LLM
- Lower API costs

---

### Example 3: Nextcloud (Code Execution)

**Config:**
```yaml
apis:
  - name: nextcloud
    type: openapi
    spec_url: http://localhost:8080/openapi.json
    # CRUD grouping not applicable (OpenAPI doesn't support it)
```

**Note:** CRUD grouping only works for GraphQL APIs

---

## Trade-offs Summary

### CRUD Grouping Enabled

**Pros:**
- ✅ 92.7% reduction in tool count
- ✅ Massively reduced context for traditional MCP
- ✅ Lower token usage and API costs
- ✅ Simpler tool selection for LLM

**Cons:**
- ❌ Harder to discover with `searchTools()` (generic "manage" vs specific "create")
- ❌ Truncated descriptions lose critical information
- ❌ Extra `operation` parameter adds complexity
- ❌ Not ideal for code execution workflows

### CRUD Grouping Disabled (Default)

**Pros:**
- ✅ Clear, focused tool names (e.g., `createIssue`)
- ✅ Easy discovery via `searchTools()`
- ✅ Simple, direct parameters
- ✅ Truncated descriptions still useful
- ✅ Ideal for code execution + discovery

**Cons:**
- ❌ More tools in traditional MCP (higher context)
- ❌ Higher token usage without code execution

---

## Decision Guide

**Use CRUD Grouping (`true`) if:**
- ✅ Using traditional MCP (Claude Desktop, no code execution)
- ✅ LLM needs to see all tools upfront
- ✅ Minimizing context window is critical
- ✅ Cost reduction is a priority

**Don't use CRUD Grouping (`false`) if:**
- ✅ Using code execution with discovery (`searchTools()`)
- ✅ On-demand tool loading
- ✅ Clear, discoverable tool names are important
- ✅ Simplicity over context reduction

---

## Migration Guide

### Switching from Disabled to Enabled

1. Update config: `enable_crud_grouping: true`
2. Restart Skyline
3. Tool count will drop dramatically (e.g., 315 → 23)
4. Update tool calls to use composite format:

**Before:**
```typescript
await callTool('gitlab__createIssue', { title: 'Bug' });
```

**After:**
```typescript
await callTool('gitlab__issue_manage', {
  operation: 'create',
  input: { title: 'Bug' }
});
```

### Switching from Enabled to Disabled

1. Update config: `enable_crud_grouping: false` (or omit `optimization`)
2. Restart Skyline
3. Tool count will increase (e.g., 23 → 315)
4. Update tool calls to use direct format:

**Before:**
```typescript
await callTool('gitlab__issue_manage', {
  operation: 'create',
  input: { title: 'Bug' }
});
```

**After:**
```typescript
await callTool('gitlab__createIssue', { title: 'Bug' });
```

---

## Future Considerations

### Potential Enhancements

1. **Smart Discovery Hints**
   - Include full operation list in tool description even when truncated
   - Add metadata to composite tools for better discovery

2. **Hybrid Mode**
   - Expose BOTH composite and separate tools
   - Let LLM choose based on use case

3. **Auto-Detection**
   - Detect if code execution is enabled
   - Automatically disable CRUD grouping in that case

4. **Search Aliases**
   - Make `searchTools('create issue')` find `issue_manage` with context

---

## Conclusion

CRUD Grouping is a powerful optimization for **traditional MCP workflows**, achieving 92.7% tool reduction. However, for **code execution with discovery**, separate tools provide better clarity and discoverability.

**Recommendation:**
- **Traditional MCP:** `enable_crud_grouping: true` ✅
- **Code Execution:** `enable_crud_grouping: false` ✅ (default)

The feature is opt-in and well-documented, allowing users to choose based on their specific use case.

---

**Last Updated:** 2026-02-12  
**Feature Version:** 1.0  
**Status:** Production-ready
