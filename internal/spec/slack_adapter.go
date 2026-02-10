package spec

import (
	"context"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/parsers/slack"
)

type SlackAdapter struct{}

func NewSlackAdapter() *SlackAdapter {
	return &SlackAdapter{}
}

func (a *SlackAdapter) Name() string { return "slack" }

func (a *SlackAdapter) Detect(raw []byte) bool {
	return slack.LooksLikeSlack(raw)
}

func (a *SlackAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return slack.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
