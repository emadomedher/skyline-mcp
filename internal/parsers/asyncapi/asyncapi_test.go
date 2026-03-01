package asyncapi

import (
	"context"
	"testing"
)

const asyncAPI2Doc = `{
  "asyncapi": "2.6.0",
  "info": {"title": "Events", "version": "1.0.0"},
  "servers": {"production": {"url": "wss://events.example.com", "protocol": "websocket"}},
  "channels": {
    "user/signedup": {
      "subscribe": {"summary": "User signed up event", "message": {"payload": {"type": "object"}}},
      "publish": {"summary": "Publish sign-up", "message": {"payload": {"type": "object"}}}
    }
  }
}`

const asyncAPI3Doc = `{
  "asyncapi": "3.0.0",
  "info": {"title": "Events V3", "version": "1.0.0"},
  "operations": {
    "onUserSignup": {
      "action": "receive",
      "channel": {"$ref": "#/channels/userSignup"},
      "summary": "User signup notification"
    },
    "sendWelcome": {
      "action": "send",
      "channel": {"$ref": "#/channels/welcomeEmail"},
      "summary": "Send welcome email"
    }
  }
}`

func TestLooksLikeAsyncAPI(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"asyncapi 2.x json", `{"asyncapi": "2.6.0"}`, true},
		{"asyncapi 3.x json", `{"asyncapi": "3.0.0"}`, true},
		{"asyncapi yaml key", "asyncapi: 2.6.0\ninfo:", true},
		{"openapi doc", `{"openapi":"3.0.0"}`, false},
		{"plain text", "Hello world", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeAsyncAPI([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("LooksLikeAsyncAPI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseToCanonical_V2(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(asyncAPI2Doc), "events", "")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}

	if svc.Name != "events" {
		t.Errorf("Name = %q, want %q", svc.Name, "events")
	}
	if svc.BaseURL != "wss://events.example.com" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "wss://events.example.com")
	}

	if len(svc.Operations) != 2 {
		t.Fatalf("len(Operations) = %d, want 2", len(svc.Operations))
	}

	ops := map[string]struct{ Method, Path string }{}
	for _, op := range svc.Operations {
		ops[op.ID] = struct{ Method, Path string }{op.Method, op.Path}
	}

	// subscribe direction => "get", publish direction => "post"
	// operationID = direction + "_" + sanitizeName(channelName)
	// sanitizeName("user/signedup") => "user_signedup"
	if op, ok := ops["subscribe_user_signedup"]; !ok {
		t.Errorf("missing subscribe_user_signedup; have %v", ops)
	} else {
		if op.Method != "get" {
			t.Errorf("subscribe method = %q, want %q", op.Method, "get")
		}
		if op.Path != "/user/signedup" {
			t.Errorf("subscribe path = %q, want %q", op.Path, "/user/signedup")
		}
	}

	if op, ok := ops["publish_user_signedup"]; !ok {
		t.Errorf("missing publish_user_signedup; have %v", ops)
	} else {
		if op.Method != "post" {
			t.Errorf("publish method = %q, want %q", op.Method, "post")
		}
	}
}

func TestParseToCanonical_V3(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(asyncAPI3Doc), "events-v3", "")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}

	if svc.Name != "events-v3" {
		t.Errorf("Name = %q, want %q", svc.Name, "events-v3")
	}
	// No servers defined, fallback to localhost.
	if svc.BaseURL != "https://localhost" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://localhost")
	}

	if len(svc.Operations) != 2 {
		t.Fatalf("len(Operations) = %d, want 2", len(svc.Operations))
	}

	ops := map[string]struct{ Method, Path string }{}
	for _, op := range svc.Operations {
		ops[op.ID] = struct{ Method, Path string }{op.Method, op.Path}
	}

	// V3: operationID = sanitizeName(opName)
	// "receive" action => "get", "send" action => "post"
	if op, ok := ops["onUserSignup"]; !ok {
		t.Errorf("missing onUserSignup; have %v", ops)
	} else {
		if op.Method != "get" {
			t.Errorf("onUserSignup method = %q, want %q", op.Method, "get")
		}
		// path = "/" + TrimPrefix("#/channels/userSignup", "#/channels/") = "/userSignup"
		if op.Path != "/userSignup" {
			t.Errorf("onUserSignup path = %q, want %q", op.Path, "/userSignup")
		}
	}

	if op, ok := ops["sendWelcome"]; !ok {
		t.Errorf("missing sendWelcome; have %v", ops)
	} else {
		if op.Method != "post" {
			t.Errorf("sendWelcome method = %q, want %q", op.Method, "post")
		}
	}
}

func TestParseToCanonical_EmptyInput(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), []byte("{}"), "test", "")
	if err == nil {
		t.Error("expected error for document with no channels or operations")
	}
}

func TestParseToCanonical_InvalidJSON(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), []byte("not json at all"), "test", "")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
