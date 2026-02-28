package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/apiblueprint"
)

type APIBlueprintAdapter struct{}

func NewAPIBlueprintAdapter() *APIBlueprintAdapter { return &APIBlueprintAdapter{} }

func (a *APIBlueprintAdapter) Name() string { return "apiblueprint" }

func (a *APIBlueprintAdapter) Detect(raw []byte) bool {
	return apiblueprint.LooksLikeAPIBlueprint(raw)
}

func (a *APIBlueprintAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return apiblueprint.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
