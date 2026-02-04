package spec

import (
	"context"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/parsers/openrpc"
)

type OpenRPCAdapter struct{}

func NewOpenRPCAdapter() *OpenRPCAdapter {
	return &OpenRPCAdapter{}
}

func (a *OpenRPCAdapter) Name() string { return "openrpc" }

func (a *OpenRPCAdapter) Detect(raw []byte) bool {
	return openrpc.LooksLikeOpenRPC(raw)
}

func (a *OpenRPCAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return openrpc.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
