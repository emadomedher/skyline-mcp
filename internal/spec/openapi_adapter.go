package spec

import (
	"context"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/openapi"
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
