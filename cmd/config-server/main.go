package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"mcp-api-bridge/internal/config"
	"mcp-api-bridge/internal/graphql"
	"mcp-api-bridge/internal/spec"
)

//go:embed ui/*
var uiFiles embed.FS

type envelope struct {
	Version    int    `yaml:"version"`
	Nonce      string `yaml:"nonce"`
	Ciphertext string `yaml:"ciphertext"`
}

type profileStore struct {
	Profiles []profile `yaml:"profiles"`
}

type profile struct {
	Name       string `yaml:"name" json:"name"`
	Token      string `yaml:"token" json:"token"`
	ConfigYAML string `yaml:"config_yaml" json:"config_yaml"`
}

type server struct {
	mu       sync.RWMutex
	store    profileStore
	path     string
	key      []byte
	authMode string
	logger   *log.Logger
}

type upsertRequest struct {
	Token      string          `json:"token"`
	ConfigYAML string          `json:"config_yaml"`
	ConfigJSON json.RawMessage `json:"config_json"`
}

func main() {
	listen := flag.String("listen", ":9190", "HTTP listen address")
	storagePath := flag.String("storage", "./profiles.enc.yaml", "Encrypted profiles storage path")
	authMode := flag.String("auth-mode", "bearer", "Auth mode: none or bearer")
	keyEnv := flag.String("key-env", "CONFIG_SERVER_KEY", "Env var name containing encryption key")
	envFile := flag.String("env-file", "", "Optional env file to load before startup")
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags)

	if *envFile != "" {
		if err := loadEnvFile(*envFile); err != nil {
			logger.Fatalf("env file: %v", err)
		}
	}

	keyRaw := os.Getenv(*keyEnv)
	if keyRaw == "" {
		logger.Fatalf("missing encryption key in %s", *keyEnv)
	}
	key, err := decodeKey(keyRaw)
	if err != nil {
		logger.Fatalf("invalid encryption key: %v", err)
	}

	mode := strings.ToLower(strings.TrimSpace(*authMode))
	if mode != "none" && mode != "bearer" {
		logger.Fatalf("unsupported auth mode %q", *authMode)
	}

	s := &server{
		path:     *storagePath,
		key:      key,
		authMode: mode,
		logger:   logger,
	}

	if err := s.load(); err != nil {
		logger.Fatalf("load store: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
	uiFS, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		logger.Fatalf("ui fs: %v", err)
	}
	mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(uiFS))))
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/profiles", s.handleProfiles)
	mux.HandleFunc("/profiles/", s.handleProfile)
	mux.HandleFunc("/detect", s.handleDetect)

	httpServer := &http.Server{
		Addr:         *listen,
		Handler:      logRequests(mux, logger),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Printf("config server listening on %s", *listen)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("server error: %v", err)
	}
}

func logRequests(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		names := make([]string, 0, len(s.store.Profiles))
		for _, p := range s.store.Profiles {
			if p.Name != "" {
				names = append(names, p.Name)
			}
		}
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{"profiles": names})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleProfile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/profiles/")
	name = strings.TrimSpace(name)
	if name == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		prof, ok := s.findProfile(name)
		s.mu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := s.authorizeProfile(r, prof); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		if strings.EqualFold(r.URL.Query().Get("format"), "json") {
			var cfg config.Config
			if err := yaml.Unmarshal([]byte(prof.ConfigYAML), &cfg); err != nil {
				http.Error(w, "invalid stored config", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"name":   prof.Name,
				"config": cfg,
			})
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		_, _ = w.Write([]byte(prof.ConfigYAML))
	case http.MethodPut:
		var req upsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		req.Token = strings.TrimSpace(req.Token)
		req.ConfigYAML = strings.TrimSpace(req.ConfigYAML)
		if len(req.ConfigJSON) > 0 {
			var cfg config.Config
			if err := json.Unmarshal(req.ConfigJSON, &cfg); err != nil {
				http.Error(w, "invalid config_json", http.StatusBadRequest)
				return
			}
			data, err := yaml.Marshal(cfg)
			if err != nil {
				http.Error(w, "failed to marshal config_json", http.StatusInternalServerError)
				return
			}
			req.ConfigYAML = strings.TrimSpace(string(data))
		}
		if req.ConfigYAML == "" {
			http.Error(w, "config_yaml or config_json is required", http.StatusBadRequest)
			return
		}
		if err := config.ValidateYAML([]byte(req.ConfigYAML)); err != nil {
			http.Error(w, fmt.Sprintf("invalid config_yaml: %v", err), http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		existing, ok := s.findProfile(name)
		if s.authMode == "bearer" {
			token := bearerToken(r.Header.Get("Authorization"))
			if ok {
				if token == "" || token != existing.Token {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			} else {
				if token == "" || token != req.Token {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}
		if req.Token == "" {
			if ok {
				req.Token = existing.Token
			} else {
				http.Error(w, "token is required", http.StatusBadRequest)
				return
			}
		}
		if ok {
			existing.Token = req.Token
			existing.ConfigYAML = req.ConfigYAML
			s.updateProfile(existing)
		} else {
			s.store.Profiles = append(s.store.Profiles, profile{
				Name:       name,
				Token:      req.Token,
				ConfigYAML: req.ConfigYAML,
			})
		}
		if err := s.save(); err != nil {
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		prof, ok := s.findProfile(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := s.authorizeProfile(r, prof); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		s.deleteProfile(name)
		if err := s.save(); err != nil {
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) authorizeProfile(r *http.Request, prof profile) error {
	if s.authMode != "bearer" {
		return nil
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" || token != prof.Token {
		return fmt.Errorf("unauthorized")
	}
	return nil
}

const graphqlIntrospectionPayload = `{"query":"query IntrospectionQuery { __schema { queryType { name } mutationType { name } types { kind name description fields(includeDeprecated: true) { name description args { name description defaultValue type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } inputFields { name description defaultValue type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } enumValues(includeDeprecated: true) { name } } } }"}`

type detectRequest struct {
	BaseURL string `json:"base_url"`
}

type detectResponse struct {
	BaseURL  string        `json:"base_url"`
	Online   bool          `json:"online"`
	Detected []detectProbe `json:"detected"`
}

type detectProbe struct {
	Type     string `json:"type"`
	SpecURL  string `json:"spec_url"`
	Method   string `json:"method"`
	Status   int    `json:"status"`
	Found    bool   `json:"found"`
	Error    string `json:"error,omitempty"`
	Endpoint string `json:"endpoint"`
}

func (s *server) handleDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req detectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		http.Error(w, "base_url is required", http.StatusBadRequest)
		return
	}

	resp := detectResponse{BaseURL: baseURL}

	probes := []struct {
		Type    string
		Path    string
		Method  string
		Body    []byte
		Headers map[string]string
	}{
		{Type: "jira-rest", Path: "/rest/api/3/serverInfo", Method: http.MethodGet},
		{Type: "openapi", Path: "/openapi.json", Method: http.MethodGet},
		{Type: "openapi", Path: "/openapi.yaml", Method: http.MethodGet},
		{Type: "swagger2", Path: "/swagger.json", Method: http.MethodGet},
		{Type: "swagger2", Path: "/swagger.yaml", Method: http.MethodGet},
		{Type: "wsdl", Path: "/wsdl", Method: http.MethodGet},
		{Type: "graphql", Path: "/graphql/schema", Method: http.MethodGet},
		{Type: "graphql", Path: "/graphql", Method: http.MethodPost, Body: []byte(graphqlIntrospectionPayload), Headers: map[string]string{"Content-Type": "application/json"}},
		{Type: "graphql", Path: "/api/graphql", Method: http.MethodPost, Body: []byte(graphqlIntrospectionPayload), Headers: map[string]string{"Content-Type": "application/json"}},
	}
	if basePathLooksLikeGraphQL(baseURL) {
		probes = append([]struct {
			Type    string
			Path    string
			Method  string
			Body    []byte
			Headers map[string]string
		}{
			{Type: "graphql", Path: "", Method: http.MethodPost, Body: []byte(graphqlIntrospectionPayload), Headers: map[string]string{"Content-Type": "application/json"}},
			{Type: "graphql", Path: "/schema", Method: http.MethodGet},
		}, probes...)
	}

	client := &http.Client{Timeout: 8 * time.Second}
	for _, probe := range probes {
		target := strings.TrimRight(baseURL, "/") + probe.Path
		found, status, err := s.probeURL(client, probe.Method, target, probe.Body, probe.Headers)
		item := detectProbe{
			Type:     probe.Type,
			SpecURL:  target,
			Method:   probe.Method,
			Status:   status,
			Found:    found,
			Endpoint: target,
		}
		if err != nil {
			item.Error = err.Error()
		}
		resp.Detected = append(resp.Detected, item)
		if found {
			resp.Online = true
		}
	}

	adapters := map[string]func([]byte) bool{
		"openapi":  spec.NewOpenAPIAdapter().Detect,
		"swagger2": spec.NewSwagger2Adapter().Detect,
		"graphql": func(raw []byte) bool {
			return graphql.LooksLikeGraphQLSDL(raw) || graphql.LooksLikeGraphQLIntrospection(raw)
		},
		"wsdl": spec.NewWSDLAdapter().Detect,
	}

	for i := range resp.Detected {
		if !resp.Detected[i].Found {
			continue
		}
		if resp.Detected[i].Type == "jira-rest" {
			continue
		}
		raw, err := s.fetchRaw(client, resp.Detected[i].Method, resp.Detected[i].SpecURL, resp.Detected[i].Method == http.MethodPost)
		if err != nil {
			resp.Detected[i].Found = false
			resp.Detected[i].Error = err.Error()
			continue
		}
		detectFn := adapters[resp.Detected[i].Type]
		if detectFn == nil || !detectFn(raw) {
			resp.Detected[i].Found = false
			resp.Detected[i].Error = "content did not match detected type"
		}
	}

	resp.Detected = applyJiraRestHint(resp.Detected, baseURL)

	writeJSON(w, http.StatusOK, resp)
}

func basePathLooksLikeGraphQL(baseURL string) bool {
	lower := strings.ToLower(baseURL)
	return strings.Contains(lower, "/graphql")
}

func applyJiraRestHint(detected []detectProbe, baseURL string) []detectProbe {
	if !strings.HasSuffix(strings.ToLower(baseURL), ".atlassian.net") {
		return detected
	}
	for i := range detected {
		if detected[i].Type == "jira-rest" && detected[i].Found {
			detected[i].SpecURL = "https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json"
			detected[i].Endpoint = detected[i].SpecURL
			return detected
		}
	}
	return detected
}

func (s *server) probeURL(client *http.Client, method, url string, body []byte, headers map[string]string) (bool, int, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("Accept", "application/json, text/yaml, application/yaml, application/xml, text/xml, */*")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, resp.StatusCode, nil
	}
	return true, resp.StatusCode, nil
}

func (s *server) fetchRaw(client *http.Client, method, url string, useIntrospection bool) ([]byte, error) {
	var body []byte
	if useIntrospection {
		body = []byte(graphqlIntrospectionPayload)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/yaml, application/yaml, application/xml, text/xml, */*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
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
		return err
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

func encrypt(plain, key []byte) (*envelope, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	return &envelope{
		Version:    1,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func decrypt(env envelope, key []byte) ([]byte, error) {
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plain, nil
}

func decodeKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty key")
	}
	if strings.HasPrefix(raw, "base64:") {
		raw = strings.TrimPrefix(raw, "base64:")
	}
	if strings.HasPrefix(raw, "hex:") {
		raw = strings.TrimPrefix(raw, "hex:")
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("key must be 32 bytes (raw), base64, or hex")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func loadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || bytes.HasPrefix(line, []byte("#")) {
			continue
		}
		parts := bytes.SplitN(line, []byte{'='}, 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(string(parts[0]))
		val := strings.TrimSpace(string(parts[1]))
		if key == "" {
			continue
		}
		if _, ok := os.LookupEnv(key); ok {
			continue
		}
		_ = os.Setenv(key, strings.Trim(val, `"'`))
	}
	return nil
}
