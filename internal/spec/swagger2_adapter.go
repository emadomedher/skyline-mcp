package spec

import (
	"context"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/parsers/swagger2"
)

type Swagger2Adapter struct{}

func NewSwagger2Adapter() *Swagger2Adapter {
	return &Swagger2Adapter{}
}

func (a *Swagger2Adapter) Name() string { return "swagger2" }

func (a *Swagger2Adapter) Detect(raw []byte) bool {
	return swagger2.LooksLikeSwagger2(raw)
}

func (a *Swagger2Adapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return swagger2.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
