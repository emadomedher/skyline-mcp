package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"skyline-mcp/internal/config"
)

func (p profile) ToConfig() *config.Config {
	var cfg config.Config
	_ = yaml.Unmarshal([]byte(p.ConfigYAML), &cfg)
	cfg.ApplyDefaults() // Apply default timeout (10s) and retries if not set
	return &cfg
}

func (s *server) findProfile(name string) (profile, bool) {
	for _, p := range s.store.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return profile{}, false
}

func (s *server) updateProfile(updated profile) {
	for i := range s.store.Profiles {
		if s.store.Profiles[i].Name == updated.Name {
			s.store.Profiles[i] = updated
			return
		}
	}
}

func (s *server) deleteProfile(name string) {
	out := s.store.Profiles[:0]
	for _, p := range s.store.Profiles {
		if p.Name != name {
			out = append(out, p)
		}
	}
	s.store.Profiles = out
}

func (s *server) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.store = profileStore{}
			return nil
		}
		return err
	}
	var env envelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("parse storage: %w", err)
	}
	plain, err := decrypt(env, s.key)
	if err != nil {
		return fmt.Errorf("decryption failed (wrong key or corrupted data): %w", err)
	}
	var store profileStore
	if err := yaml.Unmarshal(plain, &store); err != nil {
		return fmt.Errorf("parse store: %w", err)
	}
	s.store = store
	return nil
}

func (s *server) save() error {
	plain, err := yaml.Marshal(s.store)
	if err != nil {
		return err
	}
	env, err := encrypt(plain, s.key)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(env)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
