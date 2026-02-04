package config

import (
	"testing"
)

func TestAPIConfig_Validate_SpecURLOrSpecFile(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid with spec_url",
			cfg: Config{
				APIs: []APIConfig{
					{
						Name:    "test-api",
						SpecURL: "https://api.example.com/openapi.json",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid with spec_file",
			cfg: Config{
				APIs: []APIConfig{
					{
						Name:     "test-api",
						SpecFile: "/path/to/spec.json",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid with neither spec_url nor spec_file",
			cfg: Config{
				APIs: []APIConfig{
					{
						Name: "test-api",
					},
				},
			},
			wantErr: true,
			errMsg:  "either spec_url or spec_file is required",
		},
		{
			name: "invalid with both spec_url and spec_file",
			cfg: Config{
				APIs: []APIConfig{
					{
						Name:     "test-api",
						SpecURL:  "https://api.example.com/openapi.json",
						SpecFile: "/path/to/spec.json",
					},
				},
			},
			wantErr: true,
			errMsg:  "spec_url and spec_file are mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestAPIConfig_Validate_MissingName(t *testing.T) {
	cfg := Config{
		APIs: []APIConfig{
			{
				SpecURL: "https://api.example.com/openapi.json",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
	if !contains(err.Error(), "name is required") {
		t.Errorf("expected error containing 'name is required', got %q", err.Error())
	}
}

func TestAPIConfig_Validate_DuplicateName(t *testing.T) {
	cfg := Config{
		APIs: []APIConfig{
			{
				Name:    "test-api",
				SpecURL: "https://api1.example.com/openapi.json",
			},
			{
				Name:    "test-api",
				SpecURL: "https://api2.example.com/openapi.json",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
	if !contains(err.Error(), "duplicate name") {
		t.Errorf("expected error containing 'duplicate name', got %q", err.Error())
	}
}

func TestConfig_Validate_NoAPIs(t *testing.T) {
	cfg := Config{
		APIs: []APIConfig{},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for no apis, got nil")
	}
	if !contains(err.Error(), "no apis configured") {
		t.Errorf("expected error containing 'no apis configured', got %q", err.Error())
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	timeout := 5
	retries := 2
	cfg := Config{
		TimeoutSeconds: 0,
		Retries:        0,
		APIs: []APIConfig{
			{
				Name:           "test-api",
				SpecURL:        "https://api.example.com/openapi.json",
				TimeoutSeconds: nil,
				Retries:        nil,
			},
			{
				Name:           "test-api-2",
				SpecFile:       "/path/to/spec.json",
				TimeoutSeconds: &timeout,
				Retries:        &retries,
			},
		},
	}

	cfg.ApplyDefaults()

	if cfg.TimeoutSeconds != 10 {
		t.Errorf("expected global timeout to be 10, got %d", cfg.TimeoutSeconds)
	}

	if cfg.APIs[0].TimeoutSeconds == nil || *cfg.APIs[0].TimeoutSeconds != 10 {
		t.Errorf("expected first API timeout to be 10, got %v", cfg.APIs[0].TimeoutSeconds)
	}

	if cfg.APIs[0].Retries == nil || *cfg.APIs[0].Retries != 0 {
		t.Errorf("expected first API retries to be 0, got %v", cfg.APIs[0].Retries)
	}

	if cfg.APIs[1].TimeoutSeconds == nil || *cfg.APIs[1].TimeoutSeconds != 5 {
		t.Errorf("expected second API timeout to be 5, got %v", cfg.APIs[1].TimeoutSeconds)
	}

	if cfg.APIs[1].Retries == nil || *cfg.APIs[1].Retries != 2 {
		t.Errorf("expected second API retries to be 2, got %v", cfg.APIs[1].Retries)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
