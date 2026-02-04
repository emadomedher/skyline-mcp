package config

import (
	"fmt"
	"os"
)

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := LoadFromBytes(data)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) ExpandEnv() error {
	for i := range c.APIs {
		var err error
		c.APIs[i].SpecURL, err = ExpandEnvStrict(c.APIs[i].SpecURL)
		if err != nil {
			return fmt.Errorf("apis[%d].spec_url: %w", i, err)
		}
		c.APIs[i].SpecFile, err = ExpandEnvStrict(c.APIs[i].SpecFile)
		if err != nil {
			return fmt.Errorf("apis[%d].spec_file: %w", i, err)
		}
		c.APIs[i].BaseURLOverride, err = ExpandEnvStrict(c.APIs[i].BaseURLOverride)
		if err != nil {
			return fmt.Errorf("apis[%d].base_url_override: %w", i, err)
		}
		if c.APIs[i].Auth != nil {
			if c.APIs[i].Auth.Token != "" {
				c.APIs[i].Auth.Token, err = ExpandEnvStrict(c.APIs[i].Auth.Token)
				if err != nil {
					return fmt.Errorf("apis[%d].auth.token: %w", i, err)
				}
			}
			if c.APIs[i].Auth.Username != "" {
				c.APIs[i].Auth.Username, err = ExpandEnvStrict(c.APIs[i].Auth.Username)
				if err != nil {
					return fmt.Errorf("apis[%d].auth.username: %w", i, err)
				}
			}
			if c.APIs[i].Auth.Password != "" {
				c.APIs[i].Auth.Password, err = ExpandEnvStrict(c.APIs[i].Auth.Password)
				if err != nil {
					return fmt.Errorf("apis[%d].auth.password: %w", i, err)
				}
			}
			if c.APIs[i].Auth.Header != "" {
				c.APIs[i].Auth.Header, err = ExpandEnvStrict(c.APIs[i].Auth.Header)
				if err != nil {
					return fmt.Errorf("apis[%d].auth.header: %w", i, err)
				}
			}
			if c.APIs[i].Auth.Value != "" {
				c.APIs[i].Auth.Value, err = ExpandEnvStrict(c.APIs[i].Auth.Value)
				if err != nil {
					return fmt.Errorf("apis[%d].auth.value: %w", i, err)
				}
			}
		}
	}
	return nil
}
