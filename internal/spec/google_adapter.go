package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/googleapi"
)

type GoogleDiscoveryAdapter struct{}

func NewGoogleDiscoveryAdapter() *GoogleDiscoveryAdapter {
	return &GoogleDiscoveryAdapter{}
}

func (a *GoogleDiscoveryAdapter) Name() string { return "google-discovery" }

func (a *GoogleDiscoveryAdapter) Detect(raw []byte) bool {
	return googleapi.LooksLikeDiscovery(raw)
}

func (a *GoogleDiscoveryAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return googleapi.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
