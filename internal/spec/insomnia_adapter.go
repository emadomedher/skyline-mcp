package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/insomnia"
)

type InsomniaAdapter struct{}

func NewInsomniaAdapter() *InsomniaAdapter { return &InsomniaAdapter{} }

func (a *InsomniaAdapter) Name() string { return "insomnia" }

func (a *InsomniaAdapter) Detect(raw []byte) bool {
	return insomnia.LooksLikeInsomniaCollection(raw)
}

func (a *InsomniaAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return insomnia.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
