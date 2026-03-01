package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/circuitbreaker"
	"skyline-mcp/internal/config"
	"skyline-mcp/internal/ratelimit"
	"skyline-mcp/internal/redact"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/jhump/protoreflect/grpcreflect"
)

type Executor struct {
	client    *http.Client
	logger    *slog.Logger
	redactor  *redact.Redactor
	services  map[string]serviceConfig
	limiters  map[string]*ratelimit.Limiter
	breakers  map[string]*circuitbreaker.Breaker
	crumbMu   sync.Mutex
	crumbs    map[string]*crumbState
	grpcMu    sync.Mutex
	grpcConns map[string]*grpc.ClientConn
	oauth2Mgr *OAuth2TokenManager
}

type serviceConfig struct {
	BaseURL string
	Auth    *config.AuthConfig
	Timeout time.Duration
	Retries int
}

type Result struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        any    `json:"body"`
}

func NewExecutor(cfg *config.Config, services []*canonical.Service, logger *slog.Logger, redactor *redact.Redactor) (*Executor, error) {
	serviceMap := map[string]serviceConfig{}
	limiterMap := map[string]*ratelimit.Limiter{}
	breakerMap := map[string]*circuitbreaker.Breaker{}
	for _, api := range cfg.APIs {
		serviceMap[api.Name] = serviceConfig{
			Auth:    api.Auth,
			Timeout: time.Duration(derefInt(api.TimeoutSeconds, cfg.TimeoutSeconds)) * time.Second,
			Retries: derefInt(api.Retries, cfg.Retries),
		}
		rpm := derefInt(api.RateLimitRPM, 0)
		rph := derefInt(api.RateLimitRPH, 0)
		rpd := derefInt(api.RateLimitRPD, 0)
		if rpm > 0 || rph > 0 || rpd > 0 {
			limiterMap[api.Name] = ratelimit.New(rpm, rph, rpd)
			logger.Debug("rate limiter configured", "component", "executor", "api", api.Name, "rpm", rpm, "rph", rph, "rpd", rpd)
		}
		breakerMap[api.Name] = circuitbreaker.New(api.Name, 5, 30*time.Second)
		logger.Debug("circuit breaker configured", "component", "executor", "api", api.Name, "threshold", 5, "cooldown", "30s")
	}
	for _, svc := range services {
		cfgEntry, ok := serviceMap[svc.Name]
		if !ok {
			return nil, fmt.Errorf("service %s missing config", svc.Name)
		}
		cfgEntry.BaseURL = svc.BaseURL
		serviceMap[svc.Name] = cfgEntry
	}

	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	return &Executor{
		client: &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second,
		},
		logger:    logger,
		redactor:  redactor,
		services:  serviceMap,
		limiters:  limiterMap,
		breakers:  breakerMap,
		crumbs:    map[string]*crumbState{},
		grpcConns: map[string]*grpc.ClientConn{},
		oauth2Mgr: NewOAuth2TokenManager(),
	}, nil
}

// Close releases resources held by the Executor, including gRPC connections.
func (e *Executor) Close() error {
	e.grpcMu.Lock()
	defer e.grpcMu.Unlock()
	var firstErr error
	for addr, conn := range e.grpcConns {
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		e.logger.Debug("closed gRPC connection", "addr", addr)
	}
	e.grpcConns = map[string]*grpc.ClientConn{}
	return firstErr
}

func derefInt(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

// recordBreakerOutcome records a success or failure on the circuit breaker
// based on the upstream call result. 5xx status codes, timeouts, and connection
// errors count as failures. 4xx errors are valid API responses and do not
// trip the breaker.
func (e *Executor) recordBreakerOutcome(breaker *circuitbreaker.Breaker, result *Result, err error, apiName string) {
	if breaker == nil {
		return
	}
	prevState := breaker.State()
	if err != nil {
		breaker.RecordFailure(err)
		if prevState != circuitbreaker.Open && breaker.State() == circuitbreaker.Open {
			stats := breaker.Stats()
			e.logger.Warn("circuit breaker tripped", "component", "executor", "api", apiName, "failures", stats.ConsecutiveFails, "last_error", err)
		}
		return
	}
	if result != nil && result.Status >= 500 {
		serverErr := fmt.Errorf("HTTP %d", result.Status)
		breaker.RecordFailure(serverErr)
		if prevState != circuitbreaker.Open && breaker.State() == circuitbreaker.Open {
			stats := breaker.Stats()
			e.logger.Warn("circuit breaker tripped", "component", "executor", "api", apiName, "failures", stats.ConsecutiveFails, "last_error", serverErr)
		}
		return
	}
	breaker.RecordSuccess()
	if prevState != circuitbreaker.Closed && breaker.State() == circuitbreaker.Closed {
		e.logger.Info("circuit breaker recovered", "component", "executor", "api", apiName)
	}
}

func (e *Executor) Execute(ctx context.Context, op *canonical.Operation, args map[string]any) (*Result, error) {
	cfg, ok := e.services[op.ServiceName]
	if !ok {
		return nil, fmt.Errorf("unknown service %s", op.ServiceName)
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("base URL is missing for service %s", op.ServiceName)
	}

	// Check rate limit before any upstream call.
	if limiter, ok := e.limiters[op.ServiceName]; ok {
		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}

	// Check circuit breaker before any upstream call.
	breaker := e.breakers[op.ServiceName]
	if breaker != nil {
		if err := breaker.Allow(); err != nil {
			e.logger.Warn("circuit breaker rejected request", "component", "executor", "api", op.ServiceName, "error", err)
			return nil, err
		}
	}

	// Dispatch REST composite operations — route to the sub-operation for the given action.
	// Note: REST composite delegates back to Execute() for sub-operations, which will
	// check the circuit breaker again. That's correct — the sub-op is for the same service.
	if op.RESTComposite != nil {
		result, err := e.executeRESTComposite(ctx, op, args)
		// Don't record here — the recursive Execute call already records.
		return result, err
	}

	// Dispatch gRPC protocol to separate handler.
	if op.Protocol == "grpc" {
		result, err := e.executeGRPC(ctx, op, args, cfg)
		e.recordBreakerOutcome(breaker, result, err, op.ServiceName)
		return result, err
	}

	e.logger.Info("executing tool", "component", "executor", "tool", op.ToolName, "timeout", cfg.Timeout)
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	fullURL, err := resolveURL(cfg.BaseURL, op, args)
	if err != nil {
		return nil, err
	}
	e.logger.Debug("resolved URL", "component", "executor", "url", e.redactor.Redact(fullURL))
	parsedURL, err := url.Parse(fullURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	query := parsedURL.Query()
	headers := http.Header{}
	if op.QueryParamsObject != "" {
		if params, ok := args[op.QueryParamsObject]; ok {
			addQueryParamsFromObject(query, params)
		}
	}
	for _, param := range op.Parameters {
		value, ok := args[param.Name]
		if !ok {
			continue
		}
		// Skip auth-related parameters — these are handled by applyAuth from the profile config.
		if isAuthParam(param.In, param.Name) {
			continue
		}
		switch param.In {
		case "query":
			addQueryParam(query, param.Name, value)
		case "header":
			headers.Set(param.Name, valueToString(value))
		}
	}
	for name, value := range op.StaticHeaders {
		headers.Set(name, value)
	}
	parsedURL.RawQuery = query.Encode()

	var bodyBytes []byte
	if op.JSONRPC != nil {
		var err error
		bodyBytes, err = buildJSONRPCBody(op, args)
		if err != nil {
			return nil, err
		}
	} else if op.GraphQL != nil {
		var err error
		bodyBytes, err = buildGraphQLBody(op, args)
		if err != nil {
			return nil, err
		}
	} else if op.RequestBody != nil {
		bodyVal, ok := args["body"]
		if !ok {
			if op.SoapNamespace != "" {
				params := map[string]string{}
				if paramsVal, ok := args["parameters"]; ok {
					var err error
					params, err = toStringMap(paramsVal)
					if err != nil {
						return nil, fmt.Errorf("invalid parameters: %w", err)
					}
				}
				soapBody, err := buildSOAPEnvelope(op.SoapNamespace, op.ID, params)
				if err != nil {
					return nil, fmt.Errorf("build soap: %w", err)
				}
				bodyBytes = []byte(soapBody)
			} else if op.RequestBody.Required {
				return nil, fmt.Errorf("missing required request body")
			}
		} else {
			if strings.Contains(op.RequestBody.ContentType, "json") || op.RequestBody.ContentType == "" {
				encoded, err := json.Marshal(bodyVal)
				if err != nil {
					return nil, fmt.Errorf("encode request body: %w", err)
				}
				bodyBytes = encoded
			} else {
				switch v := bodyVal.(type) {
				case string:
					bodyBytes = []byte(v)
				case []byte:
					bodyBytes = v
				default:
					return nil, fmt.Errorf("request body must be string for content type %s", op.RequestBody.ContentType)
				}
			}
		}
	}

	method := strings.ToUpper(op.Method)
	attempts := cfg.Retries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		for name, vals := range headers {
			for _, v := range vals {
				req.Header.Add(name, v)
			}
		}
		if op.RequestBody != nil {
			req.Header.Set("Content-Type", op.RequestBody.ContentType)
		}
		if op.RequiresCrumb {
			if field, crumb, ok, err := e.getCrumb(ctx, op.ServiceName, cfg); err != nil {
				return nil, err
			} else if ok {
				req.Header.Set(field, crumb)
			}
		}
		if err := e.applyAuth(req, op.ServiceName, cfg.Auth); err != nil {
			return nil, fmt.Errorf("apply auth: %w", err)
		}

		e.logger.Debug("HTTP request", "component", "executor", "method", method, "url", e.redactor.Redact(parsedURL.String()), "attempt", attempt+1, "max_attempts", attempts)
		resp, err := e.client.Do(req)
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		e.logger.Debug("HTTP response", "component", "executor", "status", statusCode, "error", err)

		// Handle connection-level errors (no response received).
		if err != nil {
			if attempt < attempts-1 && isRetryable(method, 0, err) {
				delay := retryDelay(attempt, 0)
				e.logger.Warn("retrying request", "component", "executor", "api", op.ServiceName, "attempt", attempt+1, "delay", delay, "status", 0, "error", e.redactor.Redact(err.Error()))
				if sleepErr := sleepContext(ctx, delay); sleepErr != nil {
					failErr := fmt.Errorf("request failed: %w", err)
					e.recordBreakerOutcome(breaker, nil, failErr, op.ServiceName)
					return nil, failErr
				}
				continue
			}
			failErr := fmt.Errorf("request failed: %w", err)
			e.recordBreakerOutcome(breaker, nil, failErr, op.ServiceName)
			return nil, failErr
		}

		result, retry, retryAfter, err := normalizeResponse(resp)
		if err != nil {
			return nil, err
		}
		if retry && attempt < attempts-1 && isRetryable(method, result.Status, nil) {
			delay := retryDelay(attempt, retryAfter)
			if retryAfter > 0 {
				slog.Debug("using Retry-After from upstream", "seconds", delay.Seconds(), "api", op.ServiceName)
			}
			e.logger.Warn("retrying request", "component", "executor", "api", op.ServiceName, "attempt", attempt+1, "delay", delay, "status", result.Status, "error", err)
			if sleepErr := sleepContext(ctx, delay); sleepErr != nil {
				e.recordBreakerOutcome(breaker, result, nil, op.ServiceName)
				return result, nil
			}
			continue
		}
		if op.SoapNamespace != "" {
			if parsed, ok := tryParseSOAP(result); ok {
				result = parsed
			}
		}
		if op.JSONRPC != nil {
			result = tryUnwrapJSONRPC(result)
		}
		e.recordBreakerOutcome(breaker, result, nil, op.ServiceName)
		return result, nil
	}
	retryErr := fmt.Errorf("request failed after retries")
	e.recordBreakerOutcome(breaker, nil, retryErr, op.ServiceName)
	return nil, retryErr
}

func buildJSONRPCBody(op *canonical.Operation, args map[string]any) ([]byte, error) {
	rpc := op.JSONRPC
	if rpc == nil {
		return nil, nil
	}
	params := map[string]any{}
	for _, p := range op.Parameters {
		if val, ok := args[p.Name]; ok {
			params[p.Name] = val
		}
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  rpc.MethodName,
		"id":      1,
	}
	if len(params) > 0 {
		payload["params"] = params
	}
	return json.Marshal(payload)
}

func tryUnwrapJSONRPC(result *Result) *Result {
	if result == nil || result.Body == nil {
		return result
	}
	m, ok := result.Body.(map[string]any)
	if !ok {
		return result
	}
	if errVal, ok := m["error"]; ok {
		return &Result{
			Status:      result.Status,
			ContentType: result.ContentType,
			Body:        map[string]any{"jsonrpc_error": errVal},
		}
	}
	if resultVal, ok := m["result"]; ok {
		return &Result{
			Status:      result.Status,
			ContentType: result.ContentType,
			Body:        resultVal,
		}
	}
	return result
}

func buildGraphQLBody(op *canonical.Operation, args map[string]any) ([]byte, error) {
	gql := op.GraphQL
	if gql == nil {
		return nil, nil
	}

	// Check if this is a composite operation
	if gql.Composite != nil {
		return buildCompositeGraphQLBody(op, args)
	}

	selection := ""
	if val, ok := args["selection"]; ok {
		selection = strings.TrimSpace(valueToString(val))
	}
	if selection == "" {
		selection = gql.DefaultSelection
	}
	if gql.RequiresSelection {
		if strings.TrimSpace(selection) == "" {
			return nil, fmt.Errorf("missing selection set")
		}
		selection = normalizeSelection(selection)
	} else if strings.TrimSpace(selection) != "" {
		return nil, fmt.Errorf("selection set is not allowed for scalar return types")
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		if key == "selection" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	varDefs := []string{}
	argPairs := []string{}
	vars := map[string]any{}
	for _, name := range keys {
		typ, ok := gql.ArgTypes[name]
		if !ok {
			continue
		}
		varDefs = append(varDefs, fmt.Sprintf("$%s: %s", name, typ))
		argPairs = append(argPairs, fmt.Sprintf("%s: $%s", name, name))
		vars[name] = args[name]
	}

	defPart := ""
	if len(varDefs) > 0 {
		defPart = "(" + strings.Join(varDefs, ", ") + ")"
	}

	fieldCall := gql.FieldName
	if len(argPairs) > 0 {
		fieldCall += "(" + strings.Join(argPairs, ", ") + ")"
	}
	if selection != "" {
		fieldCall += " " + selection
	}

	opName := fmt.Sprintf("%s_%s", gql.OperationType, gql.FieldName)
	query := fmt.Sprintf("%s %s%s { %s }", gql.OperationType, opName, defPart, fieldCall)

	payload := map[string]any{"query": query}
	if len(vars) > 0 {
		payload["variables"] = vars
	}
	return json.Marshal(payload)
}

// executeRESTComposite routes a composite REST tool call to the appropriate sub-operation
// based on the "action" parameter, then delegates to the standard Execute flow.
func (e *Executor) executeRESTComposite(ctx context.Context, op *canonical.Operation, args map[string]any) (*Result, error) {
	comp := op.RESTComposite
	if comp == nil {
		return nil, fmt.Errorf("REST composite operation missing metadata")
	}

	actionVal, ok := args["action"]
	if !ok {
		// List available actions in the error message
		actions := make([]string, 0, len(comp.Actions))
		for name := range comp.Actions {
			actions = append(actions, name)
		}
		sort.Strings(actions)
		return nil, fmt.Errorf("'action' parameter is required; available actions: %s", strings.Join(actions, ", "))
	}

	action, ok := actionVal.(string)
	if !ok {
		return nil, fmt.Errorf("'action' must be a string")
	}

	subOp, ok := comp.Actions[action]
	if !ok {
		actions := make([]string, 0, len(comp.Actions))
		for name := range comp.Actions {
			actions = append(actions, name)
		}
		sort.Strings(actions)
		return nil, fmt.Errorf("unknown action %q; available actions: %s", action, strings.Join(actions, ", "))
	}

	// Forward all args except "action" to the sub-operation
	subArgs := make(map[string]any, len(args))
	for k, v := range args {
		if k != "action" {
			subArgs[k] = v
		}
	}

	e.logger.Debug("REST composite routing", "component", "executor", "tool", op.ToolName, "action", action, "method", subOp.Method, "path", subOp.Path)
	return e.Execute(ctx, subOp, subArgs)
}

// buildCompositeGraphQLBody orchestrates multiple GraphQL mutations for CRUD composite operations
func buildCompositeGraphQLBody(op *canonical.Operation, args map[string]any) ([]byte, error) {
	comp := op.GraphQL.Composite
	if comp == nil {
		return nil, fmt.Errorf("composite operation missing metadata")
	}

	// Extract input object from args
	inputVal, hasInput := args["input"]
	inputObj := make(map[string]any)

	if hasInput {
		if inputMap, ok := inputVal.(map[string]any); ok {
			for k, v := range inputMap {
				inputObj[k] = v
			}
		}
	}

	// Check for top-level 'id' argument (backwards compat)
	if topLevelID, ok := args["id"]; ok {
		inputObj["id"] = topLevelID
	}

	// DECISION LOGIC: Determine operation based on input
	// Check for update identifiers: global 'id', or (projectPath + iid), or similar patterns
	hasUpdateID := false
	if _, ok := inputObj["id"]; ok {
		hasUpdateID = true
	}
	// GitLab-specific: projectPath + iid for updates
	if _, hasPath := inputObj["projectPath"]; hasPath {
		if _, hasIID := inputObj["iid"]; hasIID {
			hasUpdateID = true
		}
	}

	var opRef *canonical.GraphQLOpRef
	var opAlias string

	if !hasUpdateID && comp.Create != nil {
		// No update identifier = CREATE operation
		opRef = comp.Create
		opAlias = "create"
	} else if hasUpdateID && comp.Update != nil {
		// Has update identifier = UPDATE operation
		opRef = comp.Update
		opAlias = "update"

		// GitLab UPDATE quirk: Remove global 'id' if projectPath+iid provided
		// UpdateIssueInput doesn't accept 'id', only projectPath+iid
		if _, hasPath := inputObj["projectPath"]; hasPath {
			if _, hasIID := inputObj["iid"]; hasIID {
				delete(inputObj, "id")
			}
		}
	} else {
		return nil, fmt.Errorf("no suitable operation for %s (hasUpdateID=%v)", comp.Pattern, hasUpdateID)
	}

	if opRef.InputType == "" {
		return nil, fmt.Errorf("operation %s missing input type", opRef.Name)
	}

	// Build GraphQL mutation
	opName := fmt.Sprintf("composite_%s_%s", strings.ToLower(comp.Pattern), opAlias)
	varDef := fmt.Sprintf("$input: %s", opRef.InputType)

	// Default selection - include common fields
	selection := fmt.Sprintf("{ %s { id } errors }", strings.ToLower(comp.Pattern))
	if userSelection, ok := args["selection"]; ok {
		if selStr := strings.TrimSpace(valueToString(userSelection)); selStr != "" {
			selection = normalizeSelection(selStr)
		}
	}

	query := fmt.Sprintf(
		"mutation %s(%s) { %s: %s(input: $input) %s }",
		opName,
		varDef,
		opAlias,
		opRef.Name,
		selection,
	)

	payload := map[string]any{
		"query": query,
		"variables": map[string]any{
			"input": inputObj,
		},
	}

	return json.Marshal(payload)
}

func normalizeSelection(selection string) string {
	trimmed := strings.TrimSpace(selection)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	return "{ " + trimmed + " }"
}

// isAuthParam returns true for parameters that carry authentication credentials.
// These are handled by applyAuth from the profile config and should not be
// passed through from MCP client arguments.
func isAuthParam(in, name string) bool {
	n := strings.ToLower(name)
	switch in {
	case "header":
		switch n {
		case "authorization", "x-api-key", "api-key", "apikey", "private-token":
			return true
		}
	case "query":
		switch n {
		case "token", "api_key", "apikey", "access_token", "oauth_token", "private_token":
			return true
		}
	}
	return false
}

func (e *Executor) applyAuth(req *http.Request, apiName string, auth *config.AuthConfig) error {
	if auth == nil {
		return nil
	}
	switch auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case "basic":
		cred := base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + auth.Password))
		req.Header.Set("Authorization", "Basic "+cred)
	case "api-key":
		req.Header.Set(auth.Header, auth.Value)
	case "oauth2":
		token, err := e.oauth2Mgr.GetAccessToken(apiName, auth)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

func addQueryParam(values url.Values, name string, value any) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			values.Add(name, valueToString(item))
		}
	case []string:
		for _, item := range v {
			values.Add(name, item)
		}
	default:
		values.Add(name, valueToString(value))
	}
}

func addQueryParamsFromObject(values url.Values, params any) {
	switch v := params.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			addQueryParam(values, key, v[key])
		}
	case map[string]string:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			values.Add(key, v[key])
		}
	}
}

func valueToString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(value)
	}
}

var pathParamRE = regexp.MustCompile(`\{([^}]+)\}`)

func fillPath(path string, args map[string]any) (string, error) {
	matches := pathParamRE.FindAllStringSubmatchIndex(path, -1)
	if len(matches) == 0 {
		return path, nil
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(path[last:m[0]])
		name := path[m[2]:m[3]]
		val, ok := args[name]
		if !ok {
			return "", fmt.Errorf("missing required path parameter %s", name)
		}
		b.WriteString(url.PathEscape(valueToString(val)))
		last = m[1]
	}
	b.WriteString(path[last:])
	return b.String(), nil
}

func resolveURL(base string, op *canonical.Operation, args map[string]any) (string, error) {
	base = strings.TrimRight(base, "/")
	if op.DynamicURLParam == "" {
		path, err := fillPath(op.Path, args)
		if err != nil {
			return "", err
		}
		return base + path, nil
	}

	target := ""
	if val, ok := args[op.DynamicURLParam]; ok {
		target = strings.TrimSpace(valueToString(val))
	}
	if target == "" {
		path, err := fillPath(op.Path, args)
		if err != nil {
			return "", err
		}
		return base + path, nil
	}

	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return "", fmt.Errorf("invalid base URL for service %s", op.ServiceName)
	}
	targetURL, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("invalid %s URL: %w", op.DynamicURLParam, err)
	}

	var resolved *url.URL
	if targetURL.Scheme != "" || targetURL.Host != "" {
		if targetURL.Scheme == "" || targetURL.Host == "" {
			return "", fmt.Errorf("%s must be an absolute URL or relative path", op.DynamicURLParam)
		}
		if !sameHost(baseURL, targetURL) {
			return "", fmt.Errorf("%s must match service host", op.DynamicURLParam)
		}
		resolved = targetURL
	} else {
		resolved = baseURL.ResolveReference(targetURL)
	}

	pathTrim := strings.TrimRight(resolved.Path, "/")
	if op.Path != "" && !strings.HasSuffix(pathTrim, op.Path) {
		resolved.Path = pathTrim + op.Path
	}
	return resolved.String(), nil
}

func sameHost(baseURL, targetURL *url.URL) bool {
	return strings.EqualFold(baseURL.Scheme, targetURL.Scheme) && strings.EqualFold(baseURL.Host, targetURL.Host)
}

// sleepContext waits for the specified duration or until the context is
// cancelled, whichever comes first. Returns the context error if cancelled.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// isIdempotent returns true for HTTP methods that are safe to retry on any
// server error.
func isIdempotent(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

// isRetryable determines whether a failed request should be retried based on
// the HTTP method, response status code, and error. Connection errors are
// always retryable for idempotent methods. 503 and 429 are retryable for ALL
// methods (including POST) because they explicitly signal "try again later".
// Other 5xx codes (500, 502, 504) are retryable only for idempotent methods.
func isRetryable(method string, statusCode int, err error) bool {
	// Connection-level errors (no response received) — safe to retry idempotent.
	if err != nil && statusCode == 0 {
		return isIdempotent(method)
	}

	switch statusCode {
	case http.StatusServiceUnavailable, // 503
		http.StatusTooManyRequests: // 429
		// These explicitly mean "try again later" — safe for all methods.
		return true
	case http.StatusInternalServerError, // 500
		http.StatusBadGateway,     // 502
		http.StatusGatewayTimeout: // 504
		return isIdempotent(method)
	default:
		return false
	}
}

const (
	retryBaseDelay  = 500 * time.Millisecond
	retryMaxDelay   = 10 * time.Second
	retryAfterCap   = 30 * time.Second
	maxResponseSize = 50 << 20 // 50 MB — prevents OOM from unexpectedly large upstream responses
)

// retryDelay calculates the backoff delay for a given retry attempt.
// If retryAfter is non-zero (from an upstream Retry-After header), it is used
// instead of the exponential calculation (capped at retryAfterCap).
// Otherwise the delay is: min(baseDelay * 2^attempt + jitter, maxDelay).
// Attempt 0 means the first retry (second overall request).
func retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > retryAfterCap {
			return retryAfterCap
		}
		return retryAfter
	}

	exp := math.Pow(2, float64(attempt))
	delay := time.Duration(float64(retryBaseDelay) * exp)

	// Add jitter: random value in [0, baseDelay/2).
	jitter := time.Duration(rand.Int64N(int64(retryBaseDelay / 2)))
	delay += jitter

	if delay > retryMaxDelay {
		delay = retryMaxDelay
	}
	return delay
}

// parseRetryAfter extracts the delay from a Retry-After header value.
// It handles both integer seconds ("120") and HTTP-date format
// ("Mon, 02 Jan 2006 15:04:05 GMT"). Returns 0 for unparseable values.
// The result is capped at retryAfterCap.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	// Try integer seconds first.
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		d := time.Duration(seconds) * time.Second
		if d > retryAfterCap {
			return retryAfterCap
		}
		return d
	}

	// Try HTTP-date format.
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		if d > retryAfterCap {
			return retryAfterCap
		}
		return d
	}

	return 0
}

// normalizeResponse reads the HTTP response body and returns a Result.
// The second return value (retry) is true when the status code indicates the
// request may be retried (5xx or 429). The third return value carries the
// parsed Retry-After header duration (0 if absent/unparseable).
func normalizeResponse(resp *http.Response) (*Result, bool, time.Duration, error) {
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, false, 0, fmt.Errorf("read response: %w", err)
	}
	contentType := resp.Header.Get("Content-Type")
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return &Result{Status: resp.StatusCode, ContentType: contentType}, true, retryAfter, nil
	}
	if resp.StatusCode >= 400 {
		return nil, false, 0, fmt.Errorf("http error status %d", resp.StatusCode)
	}

	var body any
	if len(bodyBytes) == 0 {
		body = nil
	} else if strings.Contains(contentType, "application/json") {
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			body = string(bodyBytes)
		}
	} else if json.Unmarshal(bodyBytes, &body) == nil {
		// Some APIs return JSON with incorrect content-type; accept it.
	} else {
		body = string(bodyBytes)
	}

	return &Result{
		Status:      resp.StatusCode,
		ContentType: contentType,
		Body:        body,
	}, false, 0, nil
}

func tryParseSOAP(result *Result) (*Result, bool) {
	if result == nil || result.Body == nil {
		return result, false
	}
	body, ok := result.Body.(string)
	if !ok || strings.TrimSpace(body) == "" {
		return result, false
	}
	if !strings.Contains(strings.ToLower(result.ContentType), "xml") && !strings.HasPrefix(strings.TrimSpace(body), "<") {
		return result, false
	}
	parsed, err := parseSOAPXML(body)
	if err != nil {
		return result, false
	}
	return &Result{
		Status:      result.Status,
		ContentType: "application/json",
		Body:        parsed,
	}, true
}

type xmlNode struct {
	name     string
	children []*xmlNode
	text     strings.Builder
}

func parseSOAPXML(input string) (any, error) {
	decoder := xml.NewDecoder(strings.NewReader(input))
	var stack []*xmlNode
	var root *xmlNode
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{name: t.Name.Local}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write([]byte(t))
			}
		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				root = node
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("soap: empty document")
	}
	if body := findSOAPBody(root); body != nil {
		return buildBodyValue(body), nil
	}
	return map[string]any{root.name: buildNodeValue(root)}, nil
}

func findSOAPBody(node *xmlNode) *xmlNode {
	if strings.EqualFold(node.name, "Body") {
		return node
	}
	for _, child := range node.children {
		if found := findSOAPBody(child); found != nil {
			return found
		}
	}
	return nil
}

func buildBodyValue(body *xmlNode) any {
	if len(body.children) == 0 {
		text := strings.TrimSpace(body.text.String())
		if text == "" {
			return map[string]any{}
		}
		return text
	}
	out := map[string]any{}
	for _, child := range body.children {
		addChildValue(out, child.name, buildNodeValue(child))
	}
	return out
}

func buildNodeValue(node *xmlNode) any {
	if len(node.children) == 0 {
		return strings.TrimSpace(node.text.String())
	}
	out := map[string]any{}
	for _, child := range node.children {
		addChildValue(out, child.name, buildNodeValue(child))
	}
	if text := strings.TrimSpace(node.text.String()); text != "" {
		out["_text"] = text
	}
	return out
}

func addChildValue(out map[string]any, name string, value any) {
	if existing, ok := out[name]; ok {
		switch v := existing.(type) {
		case []any:
			out[name] = append(v, value)
		default:
			out[name] = []any{v, value}
		}
		return
	}
	out[name] = value
}

func buildSOAPEnvelope(namespace, operation string, params map[string]string) (string, error) {
	if operation == "" {
		return "", fmt.Errorf("missing operation")
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">`)
	b.WriteString(`<soapenv:Body>`)
	if namespace != "" {
		b.WriteString("<")
		b.WriteString(operation)
		b.WriteString(` xmlns="`)
		b.WriteString(escapeXML(namespace))
		b.WriteString(`">`)
	} else {
		b.WriteString("<")
		b.WriteString(operation)
		b.WriteString(">")
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeXMLElement(&b, key, params[key])
	}

	b.WriteString("</")
	b.WriteString(operation)
	b.WriteString(">")
	b.WriteString(`</soapenv:Body></soapenv:Envelope>`)
	return b.String(), nil
}

func writeXMLElement(b *strings.Builder, name, value string) {
	b.WriteString("<")
	b.WriteString(sanitizeXMLName(name))
	b.WriteString(">")
	b.WriteString(escapeXML(value))
	b.WriteString("</")
	b.WriteString(sanitizeXMLName(name))
	b.WriteString(">")
}

func escapeXML(value string) string {
	var buf strings.Builder
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

func sanitizeXMLName(input string) string {
	if input == "" {
		return "param"
	}
	var b strings.Builder
	for i, r := range input {
		if i == 0 && !isXMLNameStart(r) {
			b.WriteRune('_')
			continue
		}
		if isXMLNameChar(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func isXMLNameStart(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || r == ':'
}

func isXMLNameChar(r rune) bool {
	return isXMLNameStart(r) || (r >= '0' && r <= '9') || r == '-' || r == '.'
}

func toStringMap(value any) (map[string]string, error) {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]string{}
		for key, val := range v {
			out[key] = fmt.Sprint(val)
		}
		return out, nil
	case map[string]string:
		return v, nil
	default:
		return nil, fmt.Errorf("parameters must be an object")
	}
}

// TruncateResult limits the serialized size of a Result to maxBytes.
// If the body is a JSON array exceeding the limit, it truncates to the first N items
// with a "_truncated" marker. Otherwise it caps the serialized JSON string.
func TruncateResult(result *Result, maxBytes int) *Result {
	if result == nil || maxBytes <= 0 {
		return result
	}
	encoded, err := json.Marshal(result.Body)
	if err != nil || len(encoded) <= maxBytes {
		return result
	}

	// If body is a slice/array, truncate items
	if arr, ok := result.Body.([]any); ok {
		total := len(arr)
		// Binary search for how many items fit
		lo, hi := 0, total
		for lo < hi {
			mid := (lo + hi + 1) / 2
			subset := arr[:mid]
			wrapped := map[string]any{
				"items":      subset,
				"_truncated": true,
				"_total":     total,
			}
			trial, _ := json.Marshal(wrapped)
			if len(trial) <= maxBytes {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		if lo == 0 {
			lo = 1 // keep at least 1 item
		}
		return &Result{
			Status:      result.Status,
			ContentType: result.ContentType,
			Body: map[string]any{
				"items":      arr[:lo],
				"_truncated": true,
				"_total":     total,
			},
		}
	}

	// For non-array bodies, cap the serialized string
	truncated := string(encoded[:maxBytes])
	return &Result{
		Status:      result.Status,
		ContentType: result.ContentType,
		Body: map[string]any{
			"data":                truncated,
			"_truncated_at_bytes": maxBytes,
		},
	}
}

type crumbState struct {
	field     string
	crumb     string
	expiresAt time.Time
	disabled  bool
}

func (e *Executor) getCrumb(ctx context.Context, serviceName string, cfg serviceConfig) (string, string, bool, error) {
	now := time.Now()
	e.crumbMu.Lock()
	state := e.crumbs[serviceName]
	if state != nil {
		if state.disabled {
			e.crumbMu.Unlock()
			return "", "", false, nil
		}
		if now.Before(state.expiresAt) && state.field != "" && state.crumb != "" {
			field := state.field
			crumb := state.crumb
			e.crumbMu.Unlock()
			return field, crumb, true, nil
		}
	}
	e.crumbMu.Unlock()

	crumbURL := strings.TrimRight(cfg.BaseURL, "/") + "/crumbIssuer/api/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, crumbURL, nil)
	if err != nil {
		return "", "", false, fmt.Errorf("crumb request failed")
	}
	req.Header.Set("Accept", "application/json")
	if err := e.applyAuth(req, serviceName, cfg.Auth); err != nil {
		return "", "", false, fmt.Errorf("crumb auth: %w", err)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("crumb request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		e.crumbMu.Lock()
		e.crumbs[serviceName] = &crumbState{disabled: true}
		e.crumbMu.Unlock()
		return "", "", false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", false, fmt.Errorf("crumb request failed with status %d", resp.StatusCode)
	}
	var payload struct {
		Field string `json:"crumbRequestField"`
		Crumb string `json:"crumb"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", false, fmt.Errorf("crumb response parse failed")
	}
	if payload.Field == "" || payload.Crumb == "" {
		return "", "", false, fmt.Errorf("crumb response missing fields")
	}
	e.crumbMu.Lock()
	e.crumbs[serviceName] = &crumbState{
		field:     payload.Field,
		crumb:     payload.Crumb,
		expiresAt: now.Add(10 * time.Minute),
	}
	e.crumbMu.Unlock()
	return payload.Field, payload.Crumb, true, nil
}

// gRPC execution

func (e *Executor) getGRPCConn(target string) (*grpc.ClientConn, error) {
	e.grpcMu.Lock()
	defer e.grpcMu.Unlock()
	if conn, ok := e.grpcConns[target]; ok {
		return conn, nil
	}

	var creds credentials.TransportCredentials
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "grpcs://") {
		creds = credentials.NewTLS(&tls.Config{})
		target = strings.TrimPrefix(strings.TrimPrefix(target, "https://"), "grpcs://")
	} else {
		creds = insecure.NewCredentials()
		target = strings.TrimPrefix(target, "http://")
	}

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", target, err)
	}
	e.grpcConns[target] = conn
	return conn, nil
}

func (e *Executor) executeGRPC(ctx context.Context, op *canonical.Operation, args map[string]any, cfg serviceConfig) (*Result, error) {
	if op.GRPCMeta == nil {
		return nil, fmt.Errorf("grpc operation %s missing GRPCMeta", op.ID)
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Pass full URL to getGRPCConn which handles scheme-based TLS selection.
	target := cfg.BaseURL

	conn, err := e.getGRPCConn(target)
	if err != nil {
		return nil, err
	}

	// Use reflection to get the method descriptor.
	refClient := grpcreflect.NewClientAuto(ctx, conn)
	defer refClient.Reset()

	svcDesc, err := refClient.ResolveService(op.GRPCMeta.ServiceFullName)
	if err != nil {
		return nil, fmt.Errorf("grpc: resolve service %s: %w", op.GRPCMeta.ServiceFullName, err)
	}
	methodDesc := svcDesc.FindMethodByName(op.GRPCMeta.MethodName)
	if methodDesc == nil {
		return nil, fmt.Errorf("grpc: method %s not found in %s", op.GRPCMeta.MethodName, op.GRPCMeta.ServiceFullName)
	}

	// Build request message from args using dynamic protobuf.
	inputDesc := methodDesc.GetInputType().UnwrapMessage()
	reqMsg := dynamicpb.NewMessage(inputDesc)

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("grpc: marshal args: %w", err)
	}
	if err := protojson.Unmarshal(argsJSON, reqMsg); err != nil {
		return nil, fmt.Errorf("grpc: populate request: %w", err)
	}

	// Invoke the RPC.
	outputDesc := methodDesc.GetOutputType().UnwrapMessage()
	respMsg := dynamicpb.NewMessage(outputDesc)
	fullMethod := fmt.Sprintf("/%s/%s", op.GRPCMeta.ServiceFullName, op.GRPCMeta.MethodName)
	if err := conn.Invoke(ctx, fullMethod, reqMsg, respMsg); err != nil {
		return nil, fmt.Errorf("grpc: invoke %s: %w", fullMethod, err)
	}

	// Serialize response to JSON.
	respJSON, err := protojson.Marshal(respMsg)
	if err != nil {
		return nil, fmt.Errorf("grpc: marshal response: %w", err)
	}
	var body any
	if err := json.Unmarshal(respJSON, &body); err != nil {
		body = string(respJSON)
	}

	return &Result{
		Status:      200,
		ContentType: "application/json",
		Body:        body,
	}, nil
}
