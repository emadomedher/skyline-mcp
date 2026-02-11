package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/odata"
)

type ODataAdapter struct{}

func NewODataAdapter() *ODataAdapter {
	return &ODataAdapter{}
}

func (a *ODataAdapter) Name() string { return "odata" }

func (a *ODataAdapter) Detect(raw []byte) bool {
	return odata.LooksLikeODataMetadata(raw)
}

func (a *ODataAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return odata.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
