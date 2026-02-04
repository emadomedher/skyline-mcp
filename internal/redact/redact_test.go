package redact

import "testing"

func TestRedact(t *testing.T) {
	redactor := NewRedactor()
	redactor.AddSecrets([]string{"secret-token", "secret"})

	input := "Authorization: Bearer secret-token and password=secret"
	got := redactor.Redact(input)
	if got == input {
		t.Fatalf("expected redaction")
	}
	if got != "Authorization: Bearer [REDACTED] and password=[REDACTED]" {
		t.Fatalf("unexpected redaction: %s", got)
	}
}
