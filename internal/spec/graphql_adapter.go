package spec

import (
	"context"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/graphql"
)

type GraphQLAdapter struct{}

func NewGraphQLAdapter() *GraphQLAdapter {
	return &GraphQLAdapter{}
}

func (a *GraphQLAdapter) Name() string { return "graphql" }

func (a *GraphQLAdapter) Detect(raw []byte) bool {
	return graphql.LooksLikeGraphQLSDL(raw) || graphql.LooksLikeGraphQLIntrospection(raw)
}

func (a *GraphQLAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return graphql.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
