package slack_test

import (
	"context"
	"testing"

	"skyline-mcp/internal/parsers/slack"
)

func TestLooksLikeSlack(t *testing.T) {
	// Slack API response has "ok" field
	raw := []byte(`{"ok":true,"channels":[{"id":"C123","name":"general"}]}`)
	if !slack.LooksLikeSlack(raw) {
		t.Fatalf("expected Slack API response to be detected")
	}
}

func TestParseToCanonical(t *testing.T) {
	raw := []byte(`{"ok":true}`)
	service, err := slack.ParseToCanonical(context.Background(), raw, "slack", "https://slack.com/api")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if service.BaseURL != "https://slack.com/api" {
		t.Fatalf("unexpected base URL: %s", service.BaseURL)
	}
	// Should have 23 operations across all categories
	if len(service.Operations) != 23 {
		t.Fatalf("expected 23 operations, got %d", len(service.Operations))
	}
	
	// Check that we have operations from each category
	foundChat := false
	foundConversations := false
	foundUsers := false
	for _, op := range service.Operations {
		if op.ID == "chatPostMessage" {
			foundChat = true
		}
		if op.ID == "conversationsList" {
			foundConversations = true
		}
		if op.ID == "usersList" {
			foundUsers = true
		}
		if op.ToolName == "" {
			t.Fatalf("found operation with empty tool name: %s", op.ID)
		}
	}
	if !foundChat || !foundConversations || !foundUsers {
		t.Fatalf("missing expected operations: chat=%v, conversations=%v, users=%v", 
			foundChat, foundConversations, foundUsers)
	}
}
