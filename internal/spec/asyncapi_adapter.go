package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/asyncapi"
)

type AsyncAPIAdapter struct{}

func NewAsyncAPIAdapter() *AsyncAPIAdapter { return &AsyncAPIAdapter{} }

func (a *AsyncAPIAdapter) Name() string { return "asyncapi" }

func (a *AsyncAPIAdapter) Detect(raw []byte) bool {
	return asyncapi.LooksLikeAsyncAPI(raw)
}

func (a *AsyncAPIAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return asyncapi.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
