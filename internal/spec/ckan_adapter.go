package spec

import (
	"context"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/ckan"
)

// CKANAdapter handles CKAN open data portal APIs.
// CKAN (https://ckan.org) powers hundreds of government open data portals
// worldwide. It uses a fixed action-based JSON API with no OpenAPI spec.
type CKANAdapter struct{}

func NewCKANAdapter() *CKANAdapter { return &CKANAdapter{} }

func (a *CKANAdapter) Name() string { return "ckan" }

func (a *CKANAdapter) Detect(raw []byte) bool { return ckan.LooksLikeCKAN(raw) }

func (a *CKANAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return ckan.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
