package spec

import (
	"context"
	"strings"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/parsers/wsdl"
)

type WSDLAdapter struct{}

func NewWSDLAdapter() *WSDLAdapter {
	return &WSDLAdapter{}
}

func (a *WSDLAdapter) Name() string { return "wsdl" }

func (a *WSDLAdapter) Detect(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	return strings.Contains(lower, "<wsdl:definitions") || strings.Contains(lower, "<definitions")
}

func (a *WSDLAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return wsdl.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
