package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
)

// SpecAdapter detects and parses API specs into canonical services.
type SpecAdapter interface {
	Name() string
	Detect(raw []byte) bool
	Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error)
}
