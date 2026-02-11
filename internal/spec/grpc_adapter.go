package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
)

// GRPCAdapter is a special adapter for gRPC services.
// Unlike other adapters, gRPC uses live reflection rather than file-based spec
// detection, so Detect always returns false. gRPC services are loaded via the
// spec_type: grpc config path in LoadServices.
type GRPCAdapter struct{}

func NewGRPCAdapter() *GRPCAdapter {
	return &GRPCAdapter{}
}

func (a *GRPCAdapter) Name() string { return "grpc" }

func (a *GRPCAdapter) Detect(_ []byte) bool {
	return false // gRPC uses reflection, not file detection.
}

func (a *GRPCAdapter) Parse(_ context.Context, _ []byte, _, _ string) (*canonical.Service, error) {
	return nil, nil // Not used; gRPC parsing is done via ParseViaReflection.
}
