package runtime_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
	"skyline-mcp/internal/logging"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/runtime"
)

func TestExecutorGETQueryHeaderPath(t *testing.T) {
	infoCh := make(chan struct {
		path   string
		query  string
		header string
	}, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		infoCh <- struct {
			path   string
			query  string
			header string
		}{path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Get("X-Trace")}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	exec := newExecutor(t, server.URL, nil, 0)
	op := &canonical.Operation{
		ServiceName: "api",
		Method:      "get",
		Path:        "/items/{id}",
		Parameters: []canonical.Parameter{
			{Name: "id", In: "path", Required: true},
			{Name: "q", In: "query"},
			{Name: "X-Trace", In: "header"},
		},
	}
	result, err := exec.Execute(context.Background(), op, map[string]any{
		"id":      "123",
		"q":       "hello",
		"X-Trace": "trace-1",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	info := <-infoCh
	if info.path != "/items/123" {
		t.Fatalf("unexpected path: %s", info.path)
	}
	if info.query != "q=hello" {
		t.Fatalf("unexpected query: %s", info.query)
	}
	if info.header != "trace-1" {
		t.Fatalf("unexpected header: %s", info.header)
	}
	body := result.Body.(map[string]any)
	if body["ok"] != true {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestExecutorSOAPWithAuthAndStaticHeaders(t *testing.T) {
	bodyCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("SOAPAction") != "urn:ListPlants" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "text/xml") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(r.Body)
		bodyCh <- string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = w.Write([]byte("<plants><plant><id>1</id></plant></plants>"))
	}))
	defer server.Close()

	auth := &config.AuthConfig{Type: "bearer", Token: "test-token"}
	exec := newExecutor(t, server.URL, auth, 0)
	op := &canonical.Operation{
		ServiceName:   "api",
		Method:        "post",
		Path:          "",
		ID:            "ListPlants",
		RequestBody:   &canonical.RequestBody{Required: true, ContentType: "text/xml; charset=utf-8"},
		StaticHeaders: map[string]string{"SOAPAction": "urn:ListPlants"},
		SoapNamespace: "http://example.com/plants",
	}
	result, err := exec.Execute(context.Background(), op, map[string]any{
		"parameters": map[string]any{"name": "fern"},
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := <-bodyCh; !strings.Contains(got, "<ListPlants") || !strings.Contains(got, "<name>fern</name>") {
		t.Fatalf("unexpected body: %s", got)
	}
	if result.ContentType != "application/json" {
		t.Fatalf("unexpected content type: %s", result.ContentType)
	}
	body, ok := result.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON body")
	}
	if _, ok := body["plants"]; !ok {
		t.Fatalf("expected plants in body: %v", body)
	}
}

func TestExecutorRetriesOn500(t *testing.T) {
	var count int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&count, 1)
		if c == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	exec := newExecutor(t, server.URL, nil, 1)
	op := &canonical.Operation{
		ServiceName: "api",
		Method:      "get",
		Path:        "/items",
	}
	result, err := exec.Execute(context.Background(), op, map[string]any{})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("unexpected status: %d", result.Status)
	}
	if atomic.LoadInt32(&count) != 2 {
		t.Fatalf("expected 2 attempts, got %d", count)
	}
}

func TestExecutorDynamicURL(t *testing.T) {
	infoCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		infoCh <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	exec := newExecutor(t, server.URL, nil, 0)
	op := &canonical.Operation{
		ServiceName:     "api",
		Method:          "get",
		Path:            "/api/json",
		DynamicURLParam: "url",
	}
	_, err := exec.Execute(context.Background(), op, map[string]any{
		"url": server.URL + "/job/example/",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if path := <-infoCh; path != "/job/example/api/json" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestExecutorQueryParamsObject(t *testing.T) {
	queryCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCh <- r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	exec := newExecutor(t, server.URL, nil, 0)
	op := &canonical.Operation{
		ServiceName:       "api",
		Method:            "post",
		Path:              "/job/{job}/buildWithParameters",
		QueryParamsObject: "parameters",
	}
	_, err := exec.Execute(context.Background(), op, map[string]any{
		"job": "demo",
		"parameters": map[string]any{
			"branch": "main",
			"env":    "staging",
		},
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	raw := <-queryCh
	values, _ := url.ParseQuery(raw)
	if values.Get("branch") != "main" || values.Get("env") != "staging" {
		t.Fatalf("unexpected query: %s", raw)
	}
}

func TestExecutorCrumbForWrite(t *testing.T) {
	crumbCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"crumbRequestField": "Jenkins-Crumb",
				"crumb":             "abc123",
			})
		default:
			crumbCh <- r.Header.Get("Jenkins-Crumb")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	defer server.Close()

	exec := newExecutor(t, server.URL, nil, 0)
	op := &canonical.Operation{
		ServiceName:   "api",
		Method:        "post",
		Path:          "/job/{job}/build",
		RequiresCrumb: true,
	}
	_, err := exec.Execute(context.Background(), op, map[string]any{"job": "demo"})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := <-crumbCh; got != "abc123" {
		t.Fatalf("expected crumb header, got %q", got)
	}
}

func newExecutor(t *testing.T, baseURL string, auth *config.AuthConfig, retries int) *runtime.Executor {
	t.Helper()
	cfg := &config.Config{
		TimeoutSeconds: 2,
		Retries:        retries,
		APIs: []config.APIConfig{
			{
				Name:            "api",
				SpecURL:         "http://example.com/spec",
				BaseURLOverride: baseURL,
				Auth:            auth,
				TimeoutSeconds:  intPtr(2),
				Retries:         intPtr(retries),
			},
		},
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config invalid: %v", err)
	}

	services := []*canonical.Service{{Name: "api", BaseURL: baseURL}}
	logger := logging.Discard()
	redactor := redact.NewRedactor()
	exec, err := runtime.NewExecutor(cfg, services, logger, redactor)
	if err != nil {
		t.Fatalf("executor init failed: %v", err)
	}
	return exec
}

func intPtr(val int) *int {
	return &val
}
