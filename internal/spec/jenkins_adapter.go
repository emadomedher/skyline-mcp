package spec

import (
	"context"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/jenkins"
)

type JenkinsAdapter struct{}

func NewJenkinsAdapter() *JenkinsAdapter {
	return &JenkinsAdapter{}
}

func (a *JenkinsAdapter) Name() string { return "jenkins" }

func (a *JenkinsAdapter) Detect(raw []byte) bool {
	return jenkins.LooksLikeJenkins(raw)
}

func (a *JenkinsAdapter) Parse(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	return jenkins.ParseToCanonical(ctx, raw, apiName, baseURLOverride)
}
