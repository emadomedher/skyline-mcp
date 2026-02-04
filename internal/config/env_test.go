package config

import (
	"os"
	"testing"
)

func TestExpandEnvStrict(t *testing.T) {
	os.Setenv("TEST_TOKEN", "abc123")
	defer os.Unsetenv("TEST_TOKEN")

	got, err := ExpandEnvStrict("Bearer ${TEST_TOKEN}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Bearer abc123" {
		t.Fatalf("unexpected expansion: %s", got)
	}
}

func TestExpandEnvStrictMissing(t *testing.T) {
	_, err := ExpandEnvStrict("${MISSING_VAR}")
	if err == nil {
		t.Fatalf("expected error for missing env var")
	}
}
