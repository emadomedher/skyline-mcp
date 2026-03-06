package email

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/runtime"
)

// ServiceName is the canonical service name used for email APIs.
const ServiceName = "email"

// BuildService creates a canonical Service with email MCP tools.
// This is called from spec/loader.go when spec_type is "email".
func BuildService(apiName string, cfg *EmailConfig) *canonical.Service {
	svc := &canonical.Service{
		Name: apiName,
	}

	hasRead := cfg.ReadProtocol() != ""
	hasSend := cfg.HasSMTP()

	if hasSend {
		svc.Operations = append(svc.Operations, buildSendEmailOp(apiName))
	}
	if hasRead {
		svc.Operations = append(svc.Operations,
			buildListEmailsOp(apiName),
			buildReadEmailOp(apiName),
			buildSearchEmailsOp(apiName),
			buildListFoldersOp(apiName),
			buildMarkReadOp(apiName),
			buildDeleteEmailOp(apiName),
			buildMoveEmailOp(apiName),
		)
	}

	return svc
}

// ExecuteEmailTool dispatches an email tool call to the appropriate handler.
// pool is optional — when non-nil, IMAP connections are borrowed from it (persistent mode).
func ExecuteEmailTool(ctx context.Context, op *canonical.Operation, args map[string]any, cfg *EmailConfig, logger *slog.Logger, pool ...*IMAPPool) (*runtime.Result, error) {
	// Build IMAP client with optional pool
	var imapPool *IMAPPool
	if len(pool) > 0 {
		imapPool = pool[0]
	}

	switch op.ID {
	case "send_email":
		return executeSendEmail(cfg, args)
	case "list_emails":
		return executeListEmails(cfg, args, logger, imapPool)
	case "read_email":
		return executeReadEmail(cfg, args, logger, imapPool)
	case "search_emails":
		return executeSearchEmails(cfg, args, logger, imapPool)
	case "list_folders":
		return executeListFolders(cfg, logger, imapPool)
	case "mark_email_read":
		return executeMarkRead(cfg, args, logger, imapPool)
	case "delete_email":
		return executeDeleteEmail(cfg, args, logger, imapPool)
	case "move_email":
		return executeMoveEmail(cfg, args, logger, imapPool)
	default:
		return nil, fmt.Errorf("unknown email operation: %s", op.ID)
	}
}

// ── Tool Definitions ────────────────────────────────────────────────────────

func buildSendEmailOp(apiName string) *canonical.Operation {
	return &canonical.Operation{
		ServiceName: apiName,
		ID:          "send_email",
		ToolName:    apiName + "__send_email",
		Method:      "POST",
		Path:        "/messages",
		Summary:     "Send an email message",
		Protocol:    "email",
		ActionHint:  "send",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"to":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Recipient email addresses"},
				"cc":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "CC email addresses"},
				"subject": map[string]any{"type": "string", "description": "Email subject line"},
				"body":    map[string]any{"type": "string", "description": "Email body content"},
				"html":    map[string]any{"type": "boolean", "description": "Whether body is HTML (default: false)"},
			},
			"required": []string{"to", "subject", "body"},
		},
	}
}

func buildListEmailsOp(apiName string) *canonical.Operation {
	return &canonical.Operation{
		ServiceName: apiName,
		ID:          "list_emails",
		ToolName:    apiName + "__list_emails",
		Method:      "GET",
		Path:        "/messages",
		Summary:     "List recent emails in a folder (default: INBOX)",
		Protocol:    "email",
		ActionHint:  "list",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"folder": map[string]any{"type": "string", "description": "Folder name (default: INBOX)", "default": "INBOX"},
				"limit":  map[string]any{"type": "integer", "description": "Max messages to return (default: 20)", "default": 20},
			},
		},
	}
}

func buildReadEmailOp(apiName string) *canonical.Operation {
	return &canonical.Operation{
		ServiceName: apiName,
		ID:          "read_email",
		ToolName:    apiName + "__read_email",
		Method:      "GET",
		Path:        "/messages/{uid}",
		Summary:     "Read a specific email by UID (returns full body)",
		Protocol:    "email",
		ActionHint:  "read",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uid":    map[string]any{"type": "integer", "description": "Message UID"},
				"folder": map[string]any{"type": "string", "description": "Folder name (default: INBOX)", "default": "INBOX"},
			},
			"required": []string{"uid"},
		},
	}
}

func buildSearchEmailsOp(apiName string) *canonical.Operation {
	return &canonical.Operation{
		ServiceName: apiName,
		ID:          "search_emails",
		ToolName:    apiName + "__search_emails",
		Method:      "GET",
		Path:        "/messages",
		Summary:     "Search emails by subject or sender",
		Protocol:    "email",
		ActionHint:  "search",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":  map[string]any{"type": "string", "description": "Search query (searches subject and from fields)"},
				"folder": map[string]any{"type": "string", "description": "Folder to search (default: INBOX)", "default": "INBOX"},
				"limit":  map[string]any{"type": "integer", "description": "Max results (default: 20)", "default": 20},
			},
			"required": []string{"query"},
		},
	}
}

func buildListFoldersOp(apiName string) *canonical.Operation {
	return &canonical.Operation{
		ServiceName: apiName,
		ID:          "list_folders",
		ToolName:    apiName + "__list_folders",
		Method:      "GET",
		Path:        "/folders",
		Summary:     "List all email folders/mailboxes",
		Protocol:    "email",
		ActionHint:  "list",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func buildMarkReadOp(apiName string) *canonical.Operation {
	return &canonical.Operation{
		ServiceName: apiName,
		ID:          "mark_email_read",
		ToolName:    apiName + "__mark_email_read",
		Method:      "PATCH",
		Path:        "/messages/{uid}",
		Summary:     "Mark an email as read",
		Protocol:    "email",
		ActionHint:  "mark_read",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uid":    map[string]any{"type": "integer", "description": "Message UID"},
				"folder": map[string]any{"type": "string", "description": "Folder name (default: INBOX)", "default": "INBOX"},
			},
			"required": []string{"uid"},
		},
	}
}

func buildDeleteEmailOp(apiName string) *canonical.Operation {
	return &canonical.Operation{
		ServiceName: apiName,
		ID:          "delete_email",
		ToolName:    apiName + "__delete_email",
		Method:      "DELETE",
		Path:        "/messages/{uid}",
		Summary:     "Delete an email (mark as deleted)",
		Protocol:    "email",
		ActionHint:  "delete",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uid":    map[string]any{"type": "integer", "description": "Message UID"},
				"folder": map[string]any{"type": "string", "description": "Folder name (default: INBOX)", "default": "INBOX"},
			},
			"required": []string{"uid"},
		},
	}
}

func buildMoveEmailOp(apiName string) *canonical.Operation {
	return &canonical.Operation{
		ServiceName: apiName,
		ID:          "move_email",
		ToolName:    apiName + "__move_email",
		Method:      "POST",
		Path:        "/messages/{uid}",
		Summary:     "Move an email to a different folder",
		Protocol:    "email",
		ActionHint:  "move",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uid":         map[string]any{"type": "integer", "description": "Message UID"},
				"from_folder": map[string]any{"type": "string", "description": "Source folder (default: INBOX)", "default": "INBOX"},
				"to_folder":   map[string]any{"type": "string", "description": "Destination folder"},
			},
			"required": []string{"uid", "to_folder"},
		},
	}
}

// ── Tool Execution ──────────────────────────────────────────────────────────

func executeSendEmail(cfg *EmailConfig, args map[string]any) (*runtime.Result, error) {
	toRaw, _ := args["to"].([]any)
	var to []string
	for _, v := range toRaw {
		if s, ok := v.(string); ok {
			to = append(to, s)
		}
	}
	if len(to) == 0 {
		return nil, fmt.Errorf("at least one recipient is required")
	}

	ccRaw, _ := args["cc"].([]any)
	var cc []string
	for _, v := range ccRaw {
		if s, ok := v.(string); ok {
			cc = append(cc, s)
		}
	}

	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)
	html, _ := args["html"].(bool)

	if err := SendEmail(cfg, to, cc, subject, body, html); err != nil {
		return nil, fmt.Errorf("send email: %w", err)
	}

	return jsonResult(map[string]any{
		"status":  "sent",
		"to":      to,
		"subject": subject,
	})
}

func executeListEmails(cfg *EmailConfig, args map[string]any, logger *slog.Logger, pool *IMAPPool) (*runtime.Result, error) {
	folder, _ := args["folder"].(string)
	limit := intArg(args, "limit", 20)

	client := newIMAPClientMaybePool(cfg, pool, logger)
	messages, err := client.ListMessages(folder, limit)
	if err != nil {
		return nil, err
	}

	return jsonResult(map[string]any{
		"folder":   folderOrDefault(folder),
		"count":    len(messages),
		"messages": messages,
	})
}

func executeReadEmail(cfg *EmailConfig, args map[string]any, logger *slog.Logger, pool *IMAPPool) (*runtime.Result, error) {
	uid := uintArg(args, "uid")
	if uid == 0 {
		return nil, fmt.Errorf("uid is required")
	}
	folder, _ := args["folder"].(string)

	client := newIMAPClientMaybePool(cfg, pool, logger)
	msg, err := client.ReadMessage(folder, uid)
	if err != nil {
		return nil, err
	}

	return jsonResult(msg)
}

func executeSearchEmails(cfg *EmailConfig, args map[string]any, logger *slog.Logger, pool *IMAPPool) (*runtime.Result, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	folder, _ := args["folder"].(string)
	limit := intArg(args, "limit", 20)

	client := newIMAPClientMaybePool(cfg, pool, logger)
	messages, err := client.SearchMessages(folder, query, limit)
	if err != nil {
		return nil, err
	}

	return jsonResult(map[string]any{
		"folder":   folderOrDefault(folder),
		"query":    query,
		"count":    len(messages),
		"messages": messages,
	})
}

func executeListFolders(cfg *EmailConfig, logger *slog.Logger, pool *IMAPPool) (*runtime.Result, error) {
	client := newIMAPClientMaybePool(cfg, pool, logger)
	folders, err := client.ListFolders()
	if err != nil {
		return nil, err
	}

	return jsonResult(map[string]any{
		"count":   len(folders),
		"folders": folders,
	})
}

func executeMarkRead(cfg *EmailConfig, args map[string]any, logger *slog.Logger, pool *IMAPPool) (*runtime.Result, error) {
	uid := uintArg(args, "uid")
	if uid == 0 {
		return nil, fmt.Errorf("uid is required")
	}
	folder, _ := args["folder"].(string)

	client := newIMAPClientMaybePool(cfg, pool, logger)
	if err := client.MarkRead(folder, uid); err != nil {
		return nil, err
	}

	return jsonResult(map[string]any{"status": "marked_read", "uid": uid})
}

func executeDeleteEmail(cfg *EmailConfig, args map[string]any, logger *slog.Logger, pool *IMAPPool) (*runtime.Result, error) {
	uid := uintArg(args, "uid")
	if uid == 0 {
		return nil, fmt.Errorf("uid is required")
	}
	folder, _ := args["folder"].(string)

	client := newIMAPClientMaybePool(cfg, pool, logger)
	if err := client.DeleteMessage(folder, uid); err != nil {
		return nil, err
	}

	return jsonResult(map[string]any{"status": "deleted", "uid": uid})
}

func executeMoveEmail(cfg *EmailConfig, args map[string]any, logger *slog.Logger, pool *IMAPPool) (*runtime.Result, error) {
	uid := uintArg(args, "uid")
	if uid == 0 {
		return nil, fmt.Errorf("uid is required")
	}
	fromFolder, _ := args["from_folder"].(string)
	toFolder, _ := args["to_folder"].(string)
	if toFolder == "" {
		return nil, fmt.Errorf("to_folder is required")
	}

	client := newIMAPClientMaybePool(cfg, pool, logger)
	if err := client.MoveMessage(fromFolder, uid, toFolder); err != nil {
		return nil, err
	}

	return jsonResult(map[string]any{"status": "moved", "uid": uid, "to_folder": toFolder})
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// newIMAPClientMaybePool creates an IMAPClient, using the pool if non-nil.
func newIMAPClientMaybePool(cfg *EmailConfig, pool *IMAPPool, logger *slog.Logger) *IMAPClient {
	if pool != nil {
		return NewIMAPClientWithPool(cfg, pool, logger)
	}
	return NewIMAPClient(cfg, logger)
}

func jsonResult(v any) (*runtime.Result, error) {
	// Result.Body is any — for JSON responses, pass the structured value directly.
	// The MCP layer will marshal it when building the tool response.
	return &runtime.Result{
		Status:      200,
		ContentType: "application/json",
		Body:        v,
	}, nil
}

func intArg(args map[string]any, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return defaultVal
}

func uintArg(args map[string]any, key string) uint32 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return uint32(n)
		case int:
			return uint32(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return uint32(i)
			}
		}
	}
	return 0
}

func folderOrDefault(folder string) string {
	if folder == "" {
		return "INBOX"
	}
	return folder
}
