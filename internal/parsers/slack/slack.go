package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"mcp-api-bridge/internal/canonical"
)

// LooksLikeSlack reports whether the payload matches Slack API patterns
func LooksLikeSlack(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	
	// Check for Slack API response structure
	if trimmed[0] == '{' {
		var payload map[string]any
		if err := json.Unmarshal(trimmed, &payload); err == nil {
			// Slack API always returns "ok" field
			if _, hasOk := payload["ok"]; hasOk {
				return true
			}
		}
	}
	
	return false
}

// ParseToCanonical returns a Slack Web API service model
// Slack's OpenAPI spec has validation issues (array-based items for oneOf)
// So we manually define the most common operations
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx
	_ = raw // Not used - we manually define operations
	
	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		baseURL = "https://slack.com/api"
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	// Add all operations
	service.Operations = append(service.Operations, getChatOperations(apiName)...)
	service.Operations = append(service.Operations, getConversationsOperations(apiName)...)
	service.Operations = append(service.Operations, getUsersOperations(apiName)...)
	service.Operations = append(service.Operations, getFilesOperations(apiName)...)
	service.Operations = append(service.Operations, getReactionsOperations(apiName)...)
	service.Operations = append(service.Operations, getPinsOperations(apiName)...)
	service.Operations = append(service.Operations, getRemindersOperations(apiName)...)
	service.Operations = append(service.Operations, getChannelsOperations(apiName)...)

	return service, nil
}

// Chat API operations
func getChatOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "chatPostMessage",
			ToolName:    canonical.ToolName(apiName, "chatPostMessage"),
			Method:      "post",
			Path:        "/chat.postMessage",
			Summary:     "Send a message to a channel. Requires scope: chat:write",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID or name"}},
				{Name: "text", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "Message text (required if no blocks)"}},
				{Name: "blocks", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "JSON-encoded array of Block Kit blocks"}},
				{Name: "thread_ts", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "Thread timestamp to reply in thread"}},
				{Name: "reply_broadcast", In: "formData", Required: false, Schema: map[string]any{"type": "boolean", "description": "Broadcast thread reply to channel"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel":         map[string]any{"type": "string", "description": "Channel ID or name"},
					"text":            map[string]any{"type": "string", "description": "Message text"},
					"blocks":          map[string]any{"type": "string", "description": "JSON Block Kit blocks"},
					"thread_ts":       map[string]any{"type": "string", "description": "Thread timestamp"},
					"reply_broadcast": map[string]any{"type": "boolean", "description": "Broadcast to channel"},
				},
				"required":             []string{"channel"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "chatUpdate",
			ToolName:    canonical.ToolName(apiName, "chatUpdate"),
			Method:      "post",
			Path:        "/chat.update",
			Summary:     "Update a message. Requires scope: chat:write",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
				{Name: "ts", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Message timestamp"}},
				{Name: "text", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "New message text"}},
				{Name: "blocks", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "JSON Block Kit blocks"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{"type": "string", "description": "Channel ID"},
					"ts":      map[string]any{"type": "string", "description": "Message timestamp"},
					"text":    map[string]any{"type": "string", "description": "New message text"},
					"blocks":  map[string]any{"type": "string", "description": "JSON Block Kit blocks"},
				},
				"required":             []string{"channel", "ts"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "chatDelete",
			ToolName:    canonical.ToolName(apiName, "chatDelete"),
			Method:      "post",
			Path:        "/chat.delete",
			Summary:     "Delete a message. Requires scope: chat:write",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
				{Name: "ts", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Message timestamp"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{"type": "string", "description": "Channel ID"},
					"ts":      map[string]any{"type": "string", "description": "Message timestamp"},
				},
				"required":             []string{"channel", "ts"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "chatGetPermalink",
			ToolName:    canonical.ToolName(apiName, "chatGetPermalink"),
			Method:      "get",
			Path:        "/chat.getPermalink",
			Summary:     "Get a permanent link to a message. Requires scope: none",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
				{Name: "message_ts", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "Message timestamp"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel":    map[string]any{"type": "string", "description": "Channel ID"},
					"message_ts": map[string]any{"type": "string", "description": "Message timestamp"},
				},
				"required":             []string{"channel", "message_ts"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Conversations API operations
func getConversationsOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "conversationsList",
			ToolName:    canonical.ToolName(apiName, "conversationsList"),
			Method:      "get",
			Path:        "/conversations.list",
			Summary:     "List all channels. Requires scope: channels:read, groups:read, im:read, mpim:read",
			Parameters: []canonical.Parameter{
				{Name: "types", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Mix and match: public_channel, private_channel, mpim, im"}},
				{Name: "limit", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Max results (default 100)"}},
				{Name: "cursor", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Pagination cursor"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"types":  map[string]any{"type": "string", "description": "Channel types"},
					"limit":  map[string]any{"type": "integer", "description": "Max results"},
					"cursor": map[string]any{"type": "string", "description": "Pagination cursor"},
				},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "conversationsCreate",
			ToolName:    canonical.ToolName(apiName, "conversationsCreate"),
			Method:      "post",
			Path:        "/conversations.create",
			Summary:     "Create a channel. Requires scope: channels:manage, groups:write, im:write, mpim:write",
			Parameters: []canonical.Parameter{
				{Name: "name", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel name"}},
				{Name: "is_private", In: "formData", Required: false, Schema: map[string]any{"type": "boolean", "description": "Create private channel"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":       map[string]any{"type": "string", "description": "Channel name"},
					"is_private": map[string]any{"type": "boolean", "description": "Private channel"},
				},
				"required":             []string{"name"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "conversationsHistory",
			ToolName:    canonical.ToolName(apiName, "conversationsHistory"),
			Method:      "get",
			Path:        "/conversations.history",
			Summary:     "Fetch conversation history. Requires scope: channels:history, groups:history, im:history, mpim:history",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
				{Name: "limit", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Max messages (default 100)"}},
				{Name: "cursor", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Pagination cursor"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{"type": "string", "description": "Channel ID"},
					"limit":   map[string]any{"type": "integer", "description": "Max messages"},
					"cursor":  map[string]any{"type": "string", "description": "Pagination cursor"},
				},
				"required":             []string{"channel"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "conversationsInvite",
			ToolName:    canonical.ToolName(apiName, "conversationsInvite"),
			Method:      "post",
			Path:        "/conversations.invite",
			Summary:     "Invite users to a channel. Requires scope: channels:manage, groups:write",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
				{Name: "users", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Comma-separated user IDs"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{"type": "string", "description": "Channel ID"},
					"users":   map[string]any{"type": "string", "description": "Comma-separated user IDs"},
				},
				"required":             []string{"channel", "users"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "conversationsInfo",
			ToolName:    canonical.ToolName(apiName, "conversationsInfo"),
			Method:      "get",
			Path:        "/conversations.info",
			Summary:     "Get channel info. Requires scope: channels:read, groups:read, im:read, mpim:read",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{"type": "string", "description": "Channel ID"},
				},
				"required":             []string{"channel"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "conversationsArchive",
			ToolName:    canonical.ToolName(apiName, "conversationsArchive"),
			Method:      "post",
			Path:        "/conversations.archive",
			Summary:     "Archive a channel. Requires scope: channels:manage, groups:write",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{"type": "string", "description": "Channel ID"},
				},
				"required":             []string{"channel"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
	}
}

// Users API operations
func getUsersOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "usersList",
			ToolName:    canonical.ToolName(apiName, "usersList"),
			Method:      "get",
			Path:        "/users.list",
			Summary:     "List all users. Requires scope: users:read",
			Parameters: []canonical.Parameter{
				{Name: "limit", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Max users (default 100)"}},
				{Name: "cursor", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Pagination cursor"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit":  map[string]any{"type": "integer", "description": "Max users"},
					"cursor": map[string]any{"type": "string", "description": "Pagination cursor"},
				},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "usersInfo",
			ToolName:    canonical.ToolName(apiName, "usersInfo"),
			Method:      "get",
			Path:        "/users.info",
			Summary:     "Get user info. Requires scope: users:read",
			Parameters: []canonical.Parameter{
				{Name: "user", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "User ID"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"user": map[string]any{"type": "string", "description": "User ID"},
				},
				"required":             []string{"user"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "usersConversations",
			ToolName:    canonical.ToolName(apiName, "usersConversations"),
			Method:      "get",
			Path:        "/users.conversations",
			Summary:     "List conversations the calling user is in. Requires scope: channels:read, groups:read, im:read, mpim:read",
			Parameters: []canonical.Parameter{
				{Name: "types", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Channel types"}},
				{Name: "limit", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Max results"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"types": map[string]any{"type": "string", "description": "Channel types"},
					"limit": map[string]any{"type": "integer", "description": "Max results"},
				},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Files API operations
func getFilesOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "filesUpload",
			ToolName:    canonical.ToolName(apiName, "filesUpload"),
			Method:      "post",
			Path:        "/files.upload",
			Summary:     "Upload a file. Requires scope: files:write",
			Parameters: []canonical.Parameter{
				{Name: "channels", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "Comma-separated channel IDs"}},
				{Name: "content", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "File contents"}},
				{Name: "filename", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "Filename"}},
				{Name: "title", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "File title"}},
				{Name: "initial_comment", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "Message text"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channels":        map[string]any{"type": "string", "description": "Channel IDs"},
					"content":         map[string]any{"type": "string", "description": "File contents"},
					"filename":        map[string]any{"type": "string", "description": "Filename"},
					"title":           map[string]any{"type": "string", "description": "Title"},
					"initial_comment": map[string]any{"type": "string", "description": "Message"},
				},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "filesList",
			ToolName:    canonical.ToolName(apiName, "filesList"),
			Method:      "get",
			Path:        "/files.list",
			Summary:     "List files. Requires scope: files:read",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Filter by channel ID"}},
				{Name: "user", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Filter by user ID"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{"type": "string", "description": "Channel ID"},
					"user":    map[string]any{"type": "string", "description": "User ID"},
				},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Reactions API operations
func getReactionsOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "reactionsAdd",
			ToolName:    canonical.ToolName(apiName, "reactionsAdd"),
			Method:      "post",
			Path:        "/reactions.add",
			Summary:     "Add a reaction to a message. Requires scope: reactions:write",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
				{Name: "name", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Reaction emoji name (without ::)"}},
				{Name: "timestamp", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Message timestamp"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel":   map[string]any{"type": "string", "description": "Channel ID"},
					"name":      map[string]any{"type": "string", "description": "Emoji name"},
					"timestamp": map[string]any{"type": "string", "description": "Message timestamp"},
				},
				"required":             []string{"channel", "name", "timestamp"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "reactionsRemove",
			ToolName:    canonical.ToolName(apiName, "reactionsRemove"),
			Method:      "post",
			Path:        "/reactions.remove",
			Summary:     "Remove a reaction. Requires scope: reactions:write",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
				{Name: "name", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Reaction emoji name"}},
				{Name: "timestamp", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Message timestamp"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel":   map[string]any{"type": "string", "description": "Channel ID"},
					"name":      map[string]any{"type": "string", "description": "Emoji name"},
					"timestamp": map[string]any{"type": "string", "description": "Message timestamp"},
				},
				"required":             []string{"channel", "name", "timestamp"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
	}
}

// Pins API operations
func getPinsOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "pinsAdd",
			ToolName:    canonical.ToolName(apiName, "pinsAdd"),
			Method:      "post",
			Path:        "/pins.add",
			Summary:     "Pin a message. Requires scope: pins:write",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
				{Name: "timestamp", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Message timestamp"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel":   map[string]any{"type": "string", "description": "Channel ID"},
					"timestamp": map[string]any{"type": "string", "description": "Message timestamp"},
				},
				"required":             []string{"channel", "timestamp"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "pinsRemove",
			ToolName:    canonical.ToolName(apiName, "pinsRemove"),
			Method:      "post",
			Path:        "/pins.remove",
			Summary:     "Unpin a message. Requires scope: pins:write",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
				{Name: "timestamp", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Message timestamp"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel":   map[string]any{"type": "string", "description": "Channel ID"},
					"timestamp": map[string]any{"type": "string", "description": "Message timestamp"},
				},
				"required":             []string{"channel", "timestamp"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "pinsList",
			ToolName:    canonical.ToolName(apiName, "pinsList"),
			Method:      "get",
			Path:        "/pins.list",
			Summary:     "List pinned messages. Requires scope: pins:read",
			Parameters: []canonical.Parameter{
				{Name: "channel", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "Channel ID"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{"type": "string", "description": "Channel ID"},
				},
				"required":             []string{"channel"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Reminders API operations
func getRemindersOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "remindersAdd",
			ToolName:    canonical.ToolName(apiName, "remindersAdd"),
			Method:      "post",
			Path:        "/reminders.add",
			Summary:     "Create a reminder. Requires scope: reminders:write",
			Parameters: []canonical.Parameter{
				{Name: "text", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Reminder text"}},
				{Name: "time", In: "formData", Required: true, Schema: map[string]any{"type": "string", "description": "Time (Unix timestamp or natural language)"}},
				{Name: "user", In: "formData", Required: false, Schema: map[string]any{"type": "string", "description": "User ID (defaults to self)"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string", "description": "Reminder text"},
					"time": map[string]any{"type": "string", "description": "Time"},
					"user": map[string]any{"type": "string", "description": "User ID"},
				},
				"required":             []string{"text", "time"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		},
		{
			ServiceName: apiName,
			ID:          "remindersList",
			ToolName:    canonical.ToolName(apiName, "remindersList"),
			Method:      "get",
			Path:        "/reminders.list",
			Summary:     "List reminders. Requires scope: reminders:read",
			Parameters:  []canonical.Parameter{},
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Legacy Channels API (some apps still use this)
func getChannelsOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "channelsList",
			ToolName:    canonical.ToolName(apiName, "channelsList"),
			Method:      "get",
			Path:        "/channels.list",
			Summary:     "List public channels (legacy). Requires scope: channels:read. Use conversations.list instead.",
			Parameters: []canonical.Parameter{
				{Name: "limit", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Max channels"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "description": "Max channels"},
				},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}
