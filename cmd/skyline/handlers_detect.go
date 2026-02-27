package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/graphql"
	"skyline-mcp/internal/parsers/openrpc"
	"skyline-mcp/internal/parsers/postman"
	"skyline-mcp/internal/spec"
)

const rpcDiscoverPayload = `{"jsonrpc":"2.0","method":"rpc.discover","id":1,"params":[]}`

var graphqlIntrospectionPayload = func() string {
	b, _ := json.Marshal(map[string]string{"query": spec.GraphQLIntrospectionQuery})
	return string(b)
}()

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

	// Build auth header to forward during probing if a token was provided.
	var probeAuth map[string]string
	if tok := strings.TrimSpace(req.BearerToken); tok != "" {
		probeAuth = map[string]string{"Authorization": "Bearer " + tok}
	}

	type probe struct {
		Type        string
		Path        string
		Method      string
		Body        []byte
		Headers     map[string]string
		AllowUnauth bool // treat HTTP 401 as "found" (server exists but requires auth)
	}

	probes := []probe{
		{Type: "jira-rest", Path: "/rest/api/3/serverInfo", Method: http.MethodGet},
		// Kubernetes-specific paths — probe these first and allow 401 so we can show
		// the kubeconfig upload helper even when no token has been supplied yet.
		{Type: "swagger2", Path: "/openapi/v2", Method: http.MethodGet, AllowUnauth: true},
		{Type: "openapi", Path: "/openapi/v3", Method: http.MethodGet, AllowUnauth: true},
		{Type: "openapi", Path: "/openapi.json", Method: http.MethodGet},
		{Type: "openapi", Path: "/openapi.yaml", Method: http.MethodGet},
		{Type: "openapi", Path: "/openapi/openapi.json", Method: http.MethodGet},
		{Type: "openapi", Path: "/openapi/openapi.yaml", Method: http.MethodGet},
		{Type: "openapi", Path: "/v3/api-docs", Method: http.MethodGet},
		{Type: "swagger2", Path: "/swagger.json", Method: http.MethodGet},
		{Type: "swagger2", Path: "/swagger.yaml", Method: http.MethodGet},
		{Type: "swagger2", Path: "/swagger/swagger.json", Method: http.MethodGet},
		{Type: "swagger2", Path: "/v2/api-docs", Method: http.MethodGet},
		{Type: "wsdl", Path: "/wsdl", Method: http.MethodGet},
		{Type: "wsdl", Path: "/wsdl?wsdl", Method: http.MethodGet},
		{Type: "wsdl", Path: "/wdsl/wsdl", Method: http.MethodGet},
		{Type: "odata", Path: "/$metadata", Method: http.MethodGet},
		{Type: "odata", Path: "/odata/$metadata", Method: http.MethodGet},
		{Type: "ckan", Path: "/api/3/action/package_list", Method: http.MethodGet},
		{Type: "openrpc", Path: "/jsonrpc/openrpc.json", Method: http.MethodGet},
		{Type: "openrpc", Path: "/openrpc.json", Method: http.MethodGet},
		{Type: "openrpc", Path: "/jsonrpc", Method: http.MethodPost, Body: []byte(rpcDiscoverPayload), Headers: map[string]string{"Content-Type": "application/json"}},
		{Type: "openrpc", Path: "/rpc", Method: http.MethodPost, Body: []byte(rpcDiscoverPayload), Headers: map[string]string{"Content-Type": "application/json"}},
		{Type: "graphql", Path: "/graphql/schema", Method: http.MethodGet},
		{Type: "graphql", Path: "/graphql", Method: http.MethodPost, Body: []byte(graphqlIntrospectionPayload), Headers: map[string]string{"Content-Type": "application/json"}},
		{Type: "graphql", Path: "/api/graphql", Method: http.MethodPost, Body: []byte(graphqlIntrospectionPayload), Headers: map[string]string{"Content-Type": "application/json"}},
	}
	if basePathLooksLikeGraphQL(baseURL) {
		probes = append([]probe{
			{Type: "graphql", Path: "", Method: http.MethodPost, Body: []byte(graphqlIntrospectionPayload), Headers: map[string]string{"Content-Type": "application/json"}},
			{Type: "graphql", Path: "/schema", Method: http.MethodGet},
		}, probes...)
	}

	// mergeHeaders returns a new map with base headers overridden/extended by extra.
	mergeHeaders := func(base, extra map[string]string) map[string]string {
		if len(base) == 0 && len(extra) == 0 {
			return nil
		}
		out := make(map[string]string, len(base)+len(extra))
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	client := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional: detect probes user-supplied URLs
		},
	}
	for _, p := range probes {
		target := strings.TrimRight(baseURL, "/") + p.Path
		headers := mergeHeaders(p.Headers, probeAuth)
		found, status, err := s.probeURL(client, p.Method, target, p.Body, headers, p.AllowUnauth)
		item := detectProbe{
			Type:     p.Type,
			SpecURL:  target,
			Method:   p.Method,
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
		"wsdl":    spec.NewWSDLAdapter().Detect,
		"odata":   looksLikeODataMetadata,
		"postman": postman.LooksLikePostmanCollection,
		"openrpc": openrpc.LooksLikeOpenRPC,
	}

	for i := range resp.Detected {
		if !resp.Detected[i].Found {
			continue
		}
		if resp.Detected[i].Type == "jira-rest" {
			continue
		}
		// If the probe succeeded only because we allow 401 (server exists but requires
		// auth), skip content validation — we cannot fetch the spec without credentials.
		if resp.Detected[i].Status == http.StatusUnauthorized {
			continue
		}
		isOpenRPCDiscover := resp.Detected[i].Type == "openrpc" && resp.Detected[i].Method == http.MethodPost
		var postBody []byte
		if isOpenRPCDiscover {
			postBody = []byte(rpcDiscoverPayload)
		}
		raw, err := s.fetchRaw(client, resp.Detected[i].Method, resp.Detected[i].SpecURL, resp.Detected[i].Method == http.MethodPost && !isOpenRPCDiscover, postBody, probeAuth)
		if err != nil {
			resp.Detected[i].Found = false
			resp.Detected[i].Error = err.Error()
			continue
		}
		// For rpc.discover responses, unwrap the JSON-RPC result.
		if isOpenRPCDiscover {
			raw = unwrapJSONRPCResult(raw)
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

func (s *server) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	specURL := strings.TrimSpace(req.SpecURL)
	if specURL == "" {
		http.Error(w, "spec_url is required", http.StatusBadRequest)
		return
	}
	client := &http.Client{Timeout: 8 * time.Second}
	found, status, err := s.probeURL(client, http.MethodGet, specURL, nil, nil)
	resp := testResponse{
		SpecURL: specURL,
		Online:  found,
		Status:  status,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleOperations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req operationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	specURL := strings.TrimSpace(req.SpecURL)

	// Resolve well-known spec URLs (Slack, GitLab, Jira, etc.)
	if req.Name != "" {
		specURL = spec.ResolveWellKnownSpecURL(req.Name, specURL)
	}

	if specURL == "" {
		http.Error(w, "spec_url is required", http.StatusBadRequest)
		return
	}

	// Fetch and parse the spec
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	operations, err := s.fetchOperations(ctx, specURL, req.SpecType)
	if err != nil {
		writeJSON(w, http.StatusOK, operationsResponse{
			Error: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, operationsResponse{
		Operations: operations,
	})
}

func (s *server) fetchOperations(ctx context.Context, specURL, specType string) ([]operationInfo, error) {
	fetcher := spec.NewFetcher(30 * time.Second)

	// Fetch spec
	raw, err := fetcher.Fetch(ctx, specURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}

	// Try all adapters to detect and parse spec
	adapters := []spec.SpecAdapter{
		spec.NewOpenAPIAdapter(),
		spec.NewSwagger2Adapter(),
		spec.NewPostmanAdapter(),
		spec.NewGoogleDiscoveryAdapter(),
		spec.NewOpenRPCAdapter(),
		spec.NewGraphQLAdapter(),
		spec.NewJenkinsAdapter(),
		spec.NewWSDLAdapter(),
		spec.NewODataAdapter(),
	}

	var service *canonical.Service
	for _, adapter := range adapters {
		if !adapter.Detect(raw) {
			continue
		}
		parsed, err := adapter.Parse(ctx, raw, "temp", "")
		if err != nil {
			s.logger.Printf("adapter %T parse error: %v", adapter, err)
			continue
		}
		service = parsed
		break
	}

	if service == nil {
		return nil, fmt.Errorf("no supported spec format detected")
	}

	// Convert to operationInfo
	result := make([]operationInfo, len(service.Operations))
	for i, op := range service.Operations {
		result[i] = operationInfo{
			ID:      op.ID,
			Method:  op.Method,
			Path:    op.Path,
			Summary: op.Summary,
		}
	}

	return result, nil
}

func (s *server) probeURL(client *http.Client, method, url string, body []byte, headers map[string]string, allowUnauth ...bool) (bool, int, error) {
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
	if resp.StatusCode == http.StatusUnauthorized && len(allowUnauth) > 0 && allowUnauth[0] {
		return true, resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, resp.StatusCode, nil
	}
	return true, resp.StatusCode, nil
}

func (s *server) fetchRaw(client *http.Client, method, url string, useIntrospection bool, explicitBody []byte, extraHeaders ...map[string]string) ([]byte, error) {
	var body []byte
	if len(explicitBody) > 0 {
		body = explicitBody
	} else if useIntrospection {
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
	for _, hdrs := range extraHeaders {
		for k, v := range hdrs {
			req.Header.Set(k, v)
		}
	}
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

func unwrapJSONRPCResult(raw []byte) []byte {
	var rpcResp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &rpcResp); err == nil && len(rpcResp.Result) > 0 {
		return []byte(rpcResp.Result)
	}
	return raw
}

func looksLikeODataMetadata(raw []byte) bool {
	s := string(raw)
	return strings.Contains(s, "edmx:Edmx") || strings.Contains(s, "<edmx:DataServices") || strings.Contains(s, "oasis-open.org/odata")
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
