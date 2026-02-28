package asyncapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"skyline-mcp/internal/canonical"
)

// LooksLikeAsyncAPI reports whether raw looks like an AsyncAPI 2.x or 3.x document.
func LooksLikeAsyncAPI(raw []byte) bool {
	// Try JSON first.
	var doc struct {
		AsyncAPI string `json:"asyncapi"`
	}
	if err := json.Unmarshal(raw, &doc); err == nil && doc.AsyncAPI != "" {
		return true
	}
	// Fallback: look for asyncapi key in YAML.
	s := string(raw)
	return strings.Contains(s, "asyncapi:") || strings.Contains(s, "\"asyncapi\":")
}

// ParseToCanonical parses an AsyncAPI 2.x/3.x document into a canonical Service.
// Subscribe channels (server → client) become GET operations (MCP Resources).
// Publish channels (client → server) become POST operations (MCP Tools).
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx

	// Normalize YAML to JSON if needed.
	raw = normalizeToJSON(raw)

	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("asyncapi: decode failed: %w", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		for _, s := range doc.Servers {
			if s.URL != "" {
				baseURL = strings.TrimRight(s.URL, "/")
				break
			}
		}
	}
	if baseURL == "" {
		baseURL = "https://localhost"
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	// AsyncAPI 2.x uses channels with publish/subscribe.
	for channelName, channel := range doc.Channels {
		if channel.Subscribe != nil {
			op := buildOperation(apiName, channelName, "subscribe", channel.Subscribe, channel)
			if op != nil {
				service.Operations = append(service.Operations, op)
			}
		}
		if channel.Publish != nil {
			op := buildOperation(apiName, channelName, "publish", channel.Publish, channel)
			if op != nil {
				service.Operations = append(service.Operations, op)
			}
		}
		// AsyncAPI 3.x uses operations map instead.
	}

	// AsyncAPI 3.x operations map.
	for opName, opDef := range doc.Operations {
		action := opDef.Action
		if action == "" {
			action = "send"
		}
		channelName := ""
		if opDef.Channel != nil {
			channelName = opDef.Channel.Ref
			if channelName == "" {
				channelName = opName
			}
		}
		op := buildV3Operation(apiName, opName, action, channelName, opDef)
		if op != nil {
			service.Operations = append(service.Operations, op)
		}
	}

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("asyncapi: no channels or operations found")
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

func buildOperation(apiName, channelName, direction string, opDef *OperationDef, channel ChannelItem) *canonical.Operation {
	operationID := sanitizeName(channelName)
	if operationID == "" {
		return nil
	}
	// Prefix with direction to distinguish subscribe vs publish on the same channel.
	operationID = direction + "_" + operationID

	toolName := canonical.ToolName(apiName, operationID)

	summary := opDef.Summary
	if summary == "" {
		summary = opDef.Description
	}
	if summary == "" {
		summary = direction + " on " + channelName
	}

	// Subscribe = server sends to client → GET (read/resource).
	// Publish = client sends to server → POST (write/tool).
	method := "post"
	if direction == "subscribe" {
		method = "get"
	}

	properties := map[string]any{}
	requiredFields := []string{}
	var params []canonical.Parameter

	// Extract parameters from channel bindings/parameters.
	for paramName, paramDef := range channel.Parameters {
		schema := paramDef.Schema
		if schema == nil {
			schema = map[string]any{"type": "string"}
		}
		params = append(params, canonical.Parameter{
			Name:     paramName,
			In:       "path",
			Required: true,
			Schema:   schema,
		})
		properties[paramName] = schema
		requiredFields = append(requiredFields, paramName)
	}

	// For publish operations, add a body parameter for the message payload.
	var reqBody *canonical.RequestBody
	if direction == "publish" {
		msgSchema := extractMessageSchema(opDef.Message)
		if msgSchema != nil {
			properties["payload"] = msgSchema
			requiredFields = append(requiredFields, "payload")
		} else {
			properties["payload"] = map[string]any{"type": "object", "additionalProperties": true, "description": "Message payload"}
			requiredFields = append(requiredFields, "payload")
		}
		reqBody = &canonical.RequestBody{
			Required:    true,
			ContentType: "application/json",
			Schema:      map[string]any{"type": "object", "additionalProperties": true},
		}
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(requiredFields) > 0 {
		sort.Strings(requiredFields)
		inputSchema["required"] = requiredFields
	}

	path := "/" + strings.TrimLeft(channelName, "/")

	return &canonical.Operation{
		ServiceName: apiName,
		ID:          operationID,
		ToolName:    toolName,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Description: opDef.Description,
		Parameters:  params,
		RequestBody: reqBody,
		InputSchema: inputSchema,
	}
}

func buildV3Operation(apiName, opName, action, channelRef string, opDef V3Operation) *canonical.Operation {
	operationID := sanitizeName(opName)
	if operationID == "" {
		return nil
	}

	toolName := canonical.ToolName(apiName, operationID)

	summary := opDef.Summary
	if summary == "" {
		summary = opDef.Description
	}
	if summary == "" {
		summary = action + " " + opName
	}

	// receive = server sends to client → GET (resource).
	// send = client sends to server → POST (tool).
	method := "post"
	if action == "receive" {
		method = "get"
	}

	properties := map[string]any{}
	requiredFields := []string{}

	var reqBody *canonical.RequestBody
	if action == "send" {
		properties["payload"] = map[string]any{"type": "object", "additionalProperties": true, "description": "Message payload"}
		requiredFields = append(requiredFields, "payload")
		reqBody = &canonical.RequestBody{
			Required:    true,
			ContentType: "application/json",
			Schema:      map[string]any{"type": "object", "additionalProperties": true},
		}
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(requiredFields) > 0 {
		sort.Strings(requiredFields)
		inputSchema["required"] = requiredFields
	}

	path := "/"
	if channelRef != "" {
		// Strip $ref prefix.
		path = "/" + strings.TrimPrefix(strings.TrimPrefix(channelRef, "#/channels/"), "/")
	}

	return &canonical.Operation{
		ServiceName: apiName,
		ID:          operationID,
		ToolName:    toolName,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Description: opDef.Description,
		RequestBody: reqBody,
		InputSchema: inputSchema,
	}
}

func extractMessageSchema(msg *MessageDef) map[string]any {
	if msg == nil {
		return nil
	}
	if msg.Payload != nil {
		return msg.Payload
	}
	// OneOf messages: pick the first.
	if len(msg.OneOf) > 0 && msg.OneOf[0].Payload != nil {
		return msg.OneOf[0].Payload
	}
	return nil
}

func normalizeToJSON(raw []byte) []byte {
	// If it starts with '{' or '[', assume JSON.
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return raw
	}
	// Attempt basic YAML key detection; for full YAML support, a dedicated
	// YAML library would be needed. For now, return raw and let json.Unmarshal
	// report an error if it's truly YAML.
	return raw
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '-' || r == ' ' || r == '.' || r == '{' || r == '}':
			b.WriteRune('_')
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}

// AsyncAPI document structures (supports both 2.x and 3.x).

type Document struct {
	AsyncAPI   string                 `json:"asyncapi"`
	Info       Info                   `json:"info"`
	Servers    map[string]Server      `json:"servers"`
	Channels   map[string]ChannelItem `json:"channels"`
	Operations map[string]V3Operation `json:"operations"` // AsyncAPI 3.x
}

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type Server struct {
	URL         string `json:"url"`
	Protocol    string `json:"protocol"`
	Description string `json:"description"`
}

type ChannelItem struct {
	Description string                      `json:"description"`
	Subscribe   *OperationDef               `json:"subscribe"`
	Publish     *OperationDef               `json:"publish"`
	Parameters  map[string]ChannelParameter `json:"parameters"`
	Bindings    map[string]any              `json:"bindings"`
}

type ChannelParameter struct {
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
	Location    string         `json:"location"`
}

type OperationDef struct {
	OperationID string      `json:"operationId"`
	Summary     string      `json:"summary"`
	Description string      `json:"description"`
	Message     *MessageDef `json:"message"`
	Tags        []Tag       `json:"tags"`
}

type MessageDef struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	Description string         `json:"description"`
	Payload     map[string]any `json:"payload"`
	OneOf       []MessageDef   `json:"oneOf"`
}

type Tag struct {
	Name string `json:"name"`
}

// AsyncAPI 3.x operation.
type V3Operation struct {
	Action      string       `json:"action"` // "send" or "receive"
	Channel     *Ref         `json:"channel"`
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Messages    []MessageDef `json:"messages"`
}

type Ref struct {
	Ref string `json:"$ref"`
}
