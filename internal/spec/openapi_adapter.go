package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/openapi"
)

type OpenAPIAdapter struct{}

func NewOpenAPIAdapter() *OpenAPIAdapter {
	return &OpenAPIAdapter{}
}

func (a *OpenAPIAdapter) Name() string { return "openapi" }

func (a *OpenAPIAdapter) Detect(raw []byte) bool {
	return openapi.LooksLikeOpenAPI(raw)
}

func (a *OpenAPIAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return openapi.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
