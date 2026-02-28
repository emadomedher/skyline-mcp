package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/raml"
)

type RAMLAdapter struct{}

func NewRAMLAdapter() *RAMLAdapter { return &RAMLAdapter{} }

func (a *RAMLAdapter) Name() string { return "raml" }

func (a *RAMLAdapter) Detect(raw []byte) bool {
	return raml.LooksLikeRAML(raw)
}

func (a *RAMLAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return raml.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
