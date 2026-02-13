# Changelog

All notable changes to Skyline MCP will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.1] - 2026-02-13

### Changed
- **BREAKING: Unified binary architecture** - Merged `skyline-bin` and `skyline-server` into single `skyline` binary
- Binary now uses `--transport` flag to switch between stdio and http modes (default: http)
- Admin UI now controlled via `--admin` flag (default: enabled)
- Updated all documentation to reflect unified binary (99 references cleaned)
- Updated CI/CD workflow to build single binary for all platforms
- Simplified Makefile with unified build targets

### Removed
- **BREAKING:** Separate `skyline-server` binary (now unified with main binary)
- Deprecated systemd files (skyline-server.service, wrapper scripts, old install script)

### Fixed
- Self-update mechanism now downloads correct binary names
- Error messages now reference correct binary name
- Documentation consistency across all user-facing docs

### Migration Guide
- Replace `skyline-server` commands with `skyline`
- Default behavior: `skyline` runs HTTP + Admin UI
- For STDIO mode (coming soon): `skyline --transport stdio`
- For HTTP without UI: `skyline --admin=false`

---

## [1.0.0] - 2026-02-12

### Added
- **Multi-platform support**: Automated builds for Linux and macOS on both x86_64 and ARM64
- **Auto-detection installer**: Single command that detects your platform and downloads the right binary
- **Build from source option**: Pass `source` flag to compile locally instead of downloading binary
- **Code execution by default**: 98% cost reduction enabled out of the box (can be disabled with `enable_code_execution: false`)
- **GraphQL CRUD grouping**: 92% tool reduction through intelligent operation grouping (enabled by default)
- **GitHub Actions workflow**: Automatic binary builds on every version tag
- **Comprehensive documentation**: Complete guides for code execution, GraphQL, REST, and all API types
- **Streamable HTTP transport**: Full MCP standard compliance (2025-11-25)
- **STDIO mode**: Native support for Claude Desktop and similar clients
- **Jenkins 2.545 support**: 34 operations across 10 API categories
- **Web UI**: Profile management and API testing interface (unified binary)

### Core Features
- Universal API bridge supporting 10+ API types:
  - REST (OpenAPI, Swagger)
  - GraphQL
  - SOAP/WSDL
  - gRPC
  - JSON-RPC
  - OData v4
  - WebSocket
  - Server-Sent Events (SSE)
  - Jenkins
  - Custom HTTP
- Automatic CRUD operation detection and grouping
- Code execution engine with TypeScript generation and Deno runtime
- Discovery-assisted execution (searchTools, __interfaces, truncated hints)
- Production-ready security (auth, retries, audit logs, encryption)
- Zero-code API integration (paste spec URL, get tools)

### Documentation
- CODE-EXECUTION.md - Complete code execution guide
- STREAMABLE-HTTP.md - MCP transport documentation
- STDIO-MODE.md - Claude Desktop integration guide
- README.md - Updated with multi-platform install instructions
- Website with comprehensive guides at skyline.projex.cc

### Changed
- Default behavior: Code execution and CRUD grouping now enabled by default
- Improved error messages and debugging output
- Enhanced GraphQL introspection and composite operations
- Better authentication handling across all API types

### Performance
- 98% cost reduction with code execution (vs traditional MCP)
- 92% tool reduction with CRUD grouping (260 tools → 23 tools)
- Sub-second response times for most operations
- Efficient binary size (~15-20MB per platform)

### Security
- Apache 2.0 license with patent protection
- Encrypted credential storage
- Audit logging for all API calls
- Rate limiting and retry logic
- TLS/SSL support for all transports

### Platforms
- Linux x86_64 (amd64)
- Linux ARM64 (Raspberry Pi, ARM servers)
- macOS Intel (x86_64)
- macOS Apple Silicon (M1/M2/M3)

### Installation
```bash
# Quick install (auto-detect platform)
curl -fsSL https://skyline.projex.cc/install | bash

# Build from source
curl -fsSL https://skyline.projex.cc/install | bash -s source
```

### Links
- GitHub: https://github.com/emadomedher/skyline-mcp
- Website: https://skyline.projex.cc
- Documentation: https://skyline.projex.cc/docs
- Issues: https://github.com/emadomedher/skyline-mcp/issues

---

**Full Changelog**: https://github.com/emadomedher/skyline-mcp/commits/v1.0.0
