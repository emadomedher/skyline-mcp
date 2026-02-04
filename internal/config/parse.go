package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// LoadFromBytes parses YAML config bytes, expands env vars, applies defaults, and validates.
func LoadFromBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
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
func ValidateYAML(data []byte) error {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}
	return nil
}
