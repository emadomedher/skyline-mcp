package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/postman"
)

type PostmanAdapter struct{}

func NewPostmanAdapter() *PostmanAdapter {
	return &PostmanAdapter{}
}

func (a *PostmanAdapter) Name() string { return "postman" }

func (a *PostmanAdapter) Detect(raw []byte) bool {
	return postman.LooksLikePostmanCollection(raw)
}

func (a *PostmanAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return postman.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
