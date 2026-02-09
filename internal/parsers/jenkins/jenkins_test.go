package jenkins_test

import (
	"context"
	"testing"

	"mcp-api-bridge/internal/parsers/jenkins"
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
	raw := []byte(`{"_class":"jenkins.model.Jenkins","url":"https://ci.example.com/","mode":"NORMAL","numExecutors":2}`)
	service, err := jenkins.ParseToCanonical(context.Background(), raw, "jenkins", "")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if service.BaseURL != "https://ci.example.com" {
		t.Fatalf("unexpected base URL: %s", service.BaseURL)
	}
	// Jenkins 2.x should return enhanced operations (30+ operations)
	if len(service.Operations) < 30 {
		t.Fatalf("expected 30+ operations for Jenkins 2.x, got %d", len(service.Operations))
	}
	// Check that we have core operations
	foundGetRoot := false
	foundListJobs := false
	foundTriggerBuild := false
	for _, op := range service.Operations {
		if op.ID == "getRoot" {
			foundGetRoot = true
		}
		if op.ID == "listJobs" {
			foundListJobs = true
		}
		if op.ID == "triggerBuild" {
			foundTriggerBuild = true
		}
		if op.ToolName == "" {
			t.Fatalf("found operation with empty tool name: %s", op.ID)
		}
	}
	if !foundGetRoot || !foundListJobs || !foundTriggerBuild {
		t.Fatalf("missing expected operations: getRoot=%v, listJobs=%v, triggerBuild=%v", 
			foundGetRoot, foundListJobs, foundTriggerBuild)
	}
}

func TestParseToCanonicalJenkins1x(t *testing.T) {
	// Jenkins 1.x response (no version, no mode)
	raw := []byte(`{"_class":"hudson.model.Hudson","url":"https://ci.example.com/"}`)
	service, err := jenkins.ParseToCanonical(context.Background(), raw, "jenkins", "")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	// Should still get enhanced operations (we default to 2.x behavior)
	if len(service.Operations) < 2 {
		t.Fatalf("expected at least 2 operations, got %d", len(service.Operations))
	}
}
