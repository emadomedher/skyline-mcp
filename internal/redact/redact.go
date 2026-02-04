package redact

import (
	"strings"
)

// Redactor replaces configured secrets in strings.
type Redactor struct {
	secrets []string
}

func NewRedactor() *Redactor {
	return &Redactor{}
}

func (r *Redactor) AddSecrets(secrets []string) {
	for _, s := range secrets {
		if s == "" {
			continue
		}
		r.secrets = append(r.secrets, s)
	}
}

func (r *Redactor) Redact(input string) string {
	out := input
	for _, secret := range r.secrets {
		if secret == "" {
			continue
		}
		out = strings.ReplaceAll(out, secret, "[REDACTED]")
	}
	return out
}
