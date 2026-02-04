package jenkins_test

import (
	"context"
	"testing"

	"mcp-api-bridge/internal/jenkins"
)

func TestLooksLikeJenkinsJSON(t *testing.T) {
	raw := []byte(`{"_class":"hudson.model.Hudson","url":"https://ci.example.com/"}`)
	if !jenkins.LooksLikeJenkins(raw) {
		t.Fatalf("expected Jenkins JSON to be detected")
	}
}

func TestLooksLikeJenkinsXML(t *testing.T) {
	raw := []byte(`<hudson _class="hudson.model.Hudson"><url>https://ci.example.com/</url></hudson>`)
	if !jenkins.LooksLikeJenkins(raw) {
		t.Fatalf("expected Jenkins XML to be detected")
	}
}

func TestParseToCanonical(t *testing.T) {
	raw := []byte(`{"_class":"hudson.model.Hudson","url":"https://ci.example.com/"}`)
	service, err := jenkins.ParseToCanonical(context.Background(), raw, "jenkins", "")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if service.BaseURL != "https://ci.example.com" {
		t.Fatalf("unexpected base URL: %s", service.BaseURL)
	}
	if len(service.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(service.Operations))
	}
	if service.Operations[0].ToolName == "" || service.Operations[1].ToolName == "" {
		t.Fatalf("expected tool names")
	}
}
