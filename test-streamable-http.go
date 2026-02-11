package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const baseURL = "http://localhost:8089"

func main() {
	fmt.Println("=== Testing Skyline MCP Streamable HTTP ===\n")

	// Test 1: Initialize session
	fmt.Println("1. Initializing session (POST /mcp)...")
	sessionID, err := initialize()
	if err != nil {
		fmt.Printf("   ❌ Initialize failed: %v\n", err)
		return
	}
	fmt.Printf("   ✅ Session created: %s\n\n", sessionID)

	// Test 2: List tools
	fmt.Println("2. Listing tools (POST /mcp)...")
	tools, err := listTools(sessionID)
	if err != nil {
		fmt.Printf("   ❌ List tools failed: %v\n", err)
		return
	}
	fmt.Printf("   ✅ Found %d tools\n", len(tools))
	if len(tools) > 0 {
		fmt.Printf("      First tool: %s\n\n", tools[0]["name"])
	}

	// Test 3: Open notification stream (optional, runs in background)
	fmt.Println("3. Opening notification stream (GET /mcp)...")
	go func() {
		if err := openNotificationStream(sessionID); err != nil {
			fmt.Printf("   ⚠️  Stream error: %v\n", err)
		}
	}()
	time.Sleep(2 * time.Second) // Let stream establish
	fmt.Println("   ✅ Notification stream opened\n")

	// Test 4: Send notification request (this would be a notification, returns 202)
	fmt.Println("4. Sending notification (POST /mcp)...")
	err = sendNotification(sessionID)
	if err != nil {
		fmt.Printf("   ❌ Notification failed: %v\n", err)
	} else {
		fmt.Println("   ✅ Notification sent (HTTP 202)\n")
	}

	// Test 5: Batch request
	fmt.Println("5. Testing batch request (POST /mcp)...")
	batchResults, err := batchRequest(sessionID)
	if err != nil {
		fmt.Printf("   ❌ Batch request failed: %v\n", err)
	} else {
		fmt.Printf("   ✅ Batch completed: %d responses\n\n", len(batchResults))
	}

	// Test 6: Terminate session
	fmt.Println("6. Terminating session (DELETE /mcp)...")
	err = terminateSession(sessionID)
	if err != nil {
		fmt.Printf("   ❌ Terminate failed: %v\n", err)
	} else {
		fmt.Println("   ✅ Session terminated\n")
	}

	fmt.Println("=== All tests completed! ===")
}

func initialize() (string, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest("POST", baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("Mcp-Protocol-Version", "2025-11-25")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	// Get session ID from header
	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		return "", fmt.Errorf("no session ID returned")
	}

	// Read response body
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return sessionID, nil
}

func listTools(sessionID string) ([]map[string]interface{}, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Result struct {
			Tools []map[string]interface{} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Result.Tools, nil
}

func sendNotification(sessionID string) error {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest("POST", baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	return nil
}

func batchRequest(sessionID string) ([]map[string]interface{}, error) {
	batch := []map[string]interface{}{
		{
			"jsonrpc": "2.0",
			"id":      10,
			"method":  "tools/list",
		},
		{
			"jsonrpc": "2.0",
			"id":      11,
			"method":  "resources/list",
		},
	}

	body, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var results []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}

	return results, nil
}

func openNotificationStream(sessionID string) error {
	httpReq, err := http.NewRequest("GET", baseURL+"/mcp", nil)
	if err != nil {
		return err
	}

	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	fmt.Println("   📡 Listening for notifications...")

	// Read SSE events
	reader := bufio.NewReader(resp.Body)
	eventCount := 0
	timeout := time.After(5 * time.Second)

	for {
		select {
		case <-timeout:
			fmt.Printf("   ℹ️  Stream timeout after %d events\n", eventCount)
			return nil
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if strings.HasPrefix(line, ":") {
				// Comment (heartbeat)
				continue
			}

			if strings.HasPrefix(line, "data: ") {
				eventCount++
				data := strings.TrimPrefix(line, "data: ")
				fmt.Printf("   📩 Event #%d: %s\n", eventCount, truncate(data, 80))
			}
		}
	}
}

func terminateSession(sessionID string) error {
	httpReq, err := http.NewRequest("DELETE", baseURL+"/mcp", nil)
	if err != nil {
		return err
	}

	httpReq.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
