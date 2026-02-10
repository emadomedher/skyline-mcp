package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/config"
	"mcp-api-bridge/internal/redact"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/jhump/protoreflect/grpcreflect"
)

type Executor struct {
	client    *http.Client
	logger    *log.Logger
	redactor  *redact.Redactor
	services  map[string]serviceConfig
	crumbMu   sync.Mutex
	crumbs    map[string]*crumbState
	grpcMu    sync.Mutex
	grpcConns map[string]*grpc.ClientConn
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

func NewExecutor(cfg *config.Config, services []*canonical.Service, logger *log.Logger, redactor *redact.Redactor) (*Executor, error) {
	serviceMap := map[string]serviceConfig{}
	for _, api := range cfg.APIs {
		serviceMap[api.Name] = serviceConfig{
			Auth:    api.Auth,
			Timeout: time.Duration(derefInt(api.TimeoutSeconds, cfg.TimeoutSeconds)) * time.Second,
			Retries: derefInt(api.Retries, cfg.Retries),
		}
	}
	for _, svc := range services {
		cfgEntry, ok := serviceMap[svc.Name]
		if !ok {
			return nil, fmt.Errorf("service %s missing config", svc.Name)
		}
		cfgEntry.BaseURL = svc.BaseURL
		serviceMap[svc.Name] = cfgEntry
	}

	return &Executor{
		client:    &http.Client{},
		logger:    logger,
		redactor:  redactor,
		services:  serviceMap,
		crumbs:    map[string]*crumbState{},
		grpcConns: map[string]*grpc.ClientConn{},
	}, nil
}

func derefInt(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

func (e *Executor) Execute(ctx context.Context, op *canonical.Operation, args map[string]any) (*Result, error) {
	cfg, ok := e.services[op.ServiceName]
	if !ok {
		return nil, fmt.Errorf("unknown service %s", op.ServiceName)
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("base URL is missing for service %s", op.ServiceName)
	}

	// Dispatch gRPC protocol to separate handler.
	if op.Protocol == "grpc" {
		return e.executeGRPC(ctx, op, args, cfg)
	}

	e.logger.Printf("[EXECUTOR] Starting execute for %s (timeout=%v)", op.ToolName, cfg.Timeout)
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	fullURL, err := resolveURL(cfg.BaseURL, op, args)
	if err != nil {
		return nil, err
	}
	e.logger.Printf("[EXECUTOR] Resolved URL: %s", e.redactor.Redact(fullURL))
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
		applyAuth(req, cfg.Auth)

		e.logger.Printf("[EXECUTOR] Making HTTP request: %s %s (attempt %d/%d)", method, e.redactor.Redact(parsedURL.String()), attempt+1, attempts)
		resp, err := e.client.Do(req)
		e.logger.Printf("[EXECUTOR] HTTP request completed (err=%v, status=%v)", err, func() int {
			if resp != nil {
				return resp.StatusCode
			}
			return 0
		}())
		if err != nil {
			if attempt < attempts-1 && isRetryable(method) {
				e.logger.Printf("request failed, retrying: %s", e.redactor.Redact(err.Error()))
				continue
			}
			return nil, fmt.Errorf("request failed: %w", err)
		}
		result, retry, err := normalizeResponse(resp)
		if err != nil {
			return nil, err
		}
		if retry && attempt < attempts-1 && isRetryable(method) {
			e.logger.Printf("retrying on %d", result.Status)
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
		return result, nil
	}
	return nil, fmt.Errorf("request failed after retries")
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

func applyAuth(req *http.Request, auth *config.AuthConfig) {
	if auth == nil {
		return
	}
	switch auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case "basic":
		cred := base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + auth.Password))
		req.Header.Set("Authorization", "Basic "+cred)
	case "api-key":
		req.Header.Set(auth.Header, auth.Value)
	}
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

func isRetryable(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

func normalizeResponse(resp *http.Response) (*Result, bool, error) {
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read response: %w", err)
	}
	contentType := resp.Header.Get("Content-Type")

	if resp.StatusCode >= 500 {
		return &Result{Status: resp.StatusCode, ContentType: contentType}, true, nil
	}
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf("http error status %d", resp.StatusCode)
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
	}, false, nil
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
	applyAuth(req, cfg.Auth)

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
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
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

	// Strip scheme from target for gRPC dial.
	target := cfg.BaseURL
	target = strings.TrimPrefix(target, "http://")
	target = strings.TrimPrefix(target, "https://")

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
