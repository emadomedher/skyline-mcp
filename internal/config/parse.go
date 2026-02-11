package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// LoadFromBytes parses YAML or JSON config bytes, expands env vars, applies defaults, and validates.
// Auto-detects format: JSON if content starts with { or [, otherwise YAML.
func LoadFromBytes(data []byte) (*Config, error) {
	var cfg Config
	
	// Auto-detect format
	trimmed := bytes.TrimSpace(data)
	isJSON := len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
	
	var err error
	if isJSON {
		// Parse as JSON
		err = json.Unmarshal(data, &cfg)
		if err != nil {
			return nil, fmt.Errorf("parse config (JSON): %w", err)
		}
	} else {
		// Parse as YAML (also handles JSON since YAML is superset)
		err = yaml.Unmarshal(data, &cfg)
		if err != nil {
			return nil, fmt.Errorf("parse config (YAML): %w", err)
		}
	}
	
	if err := cfg.ExpandEnv(); err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ValidateYAML parses YAML config bytes, applies defaults, and validates without env expansion.
// Deprecated: Use ValidateConfig for format-agnostic validation.
func ValidateYAML(data []byte) error {
	return ValidateConfig(data)
}

// ValidateConfig parses YAML or JSON config bytes, applies defaults, and validates without env expansion.
func ValidateConfig(data []byte) error {
	var cfg Config
	
	// Auto-detect format
	trimmed := bytes.TrimSpace(data)
	isJSON := len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
	
	var err error
	if isJSON {
		err = json.Unmarshal(data, &cfg)
		if err != nil {
			return fmt.Errorf("parse config (JSON): %w", err)
		}
	} else {
		err = yaml.Unmarshal(data, &cfg)
		if err != nil {
			return fmt.Errorf("parse config (YAML): %w", err)
		}
	}
	
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}
	return nil
}
