package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"skyline-mcp/internal/config"
	"skyline-mcp/internal/logging"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/runtime"
	"skyline-mcp/internal/spec"
)

func TestServerWSDLToolCall(t *testing.T) {
	wsdl := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:tns="http://example.com/plants"
  targetNamespace="http://example.com/plants">
  <service name="PlantSoapService">
    <port name="PlantSoapPort" binding="tns:PlantSoapBinding">
      <soap:address location="{{SOAP_URL}}"/>
    </port>
  </service>
  <binding name="PlantSoapBinding" type="tns:PlantSoapPortType">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="ListPlants">
      <soap:operation soapAction="urn:ListPlants"/>
      <input><soap:body use="literal"/></input>
      <output><soap:body use="literal"/></output>
    </operation>
  </binding>
</definitions>
`

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wsdl":
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write([]byte(strings.ReplaceAll(wsdl, "{{SOAP_URL}}", server.URL+"/soap")))
		case "/soap":
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("SOAPAction") != "urn:ListPlants" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write([]byte("<plants><plant><id>1</id></plant></plants>"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		APIs: []config.APIConfig{
			{
				Name:    "mocksoap",
				SpecURL: server.URL + "/wsdl",
				Auth:    &config.AuthConfig{Type: "bearer", Token: "test-token"},
			},
		},
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config validation failed: %v", err)
	}

	logger := logging.Discard()
	redactor := redact.NewRedactor()
	services, err := spec.LoadServices(context.Background(), cfg, logger, redactor)
	if err != nil {
		t.Fatalf("spec load failed: %v", err)
	}
	executor, err := runtime.NewExecutor(cfg, services, logger, redactor)
	if err != nil {
		t.Fatalf("executor init failed: %v", err)
	}
	registry, err := NewRegistry(services)
	if err != nil {
		t.Fatalf("registry init failed: %v", err)
	}

	mcpServer := NewServer(registry, executor, logger, redactor, "test")
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = mcpServer.Serve(ctx, inReader, outWriter)
		_ = outWriter.Close()
	}()

	dec := json.NewDecoder(outReader)
	send := func(payload any) {
		data, _ := json.Marshal(payload)
		_, _ = inWriter.Write(append(data, '\n'))
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	var listResp map[string]any
	if err := dec.Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	result := listResp["result"].(map[string]any)
	tools := result["tools"].([]any)
	found := false
	for _, tool := range tools {
		entry := tool.(map[string]any)
		if entry["name"] == "mocksoap__ListPlants" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected mocksoap__ListPlants tool")
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "mocksoap__ListPlants",
			"arguments": map[string]any{
				"parameters": map[string]any{},
			},
		},
	})
	var callResp map[string]any
	if err := dec.Decode(&callResp); err != nil {
		t.Fatalf("decode call response: %v", err)
	}
	callResult := callResp["result"].(map[string]any)
	content := callResult["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected single content item")
	}
	contentItem := content[0].(map[string]any)
	text := contentItem["text"].(string)
	var jsonObj map[string]any
	if err := json.Unmarshal([]byte(text), &jsonObj); err != nil {
		t.Fatalf("failed to decode tool response: %v", err)
	}
	body := jsonObj["body"].(map[string]any)
	if _, ok := body["plants"]; !ok {
		t.Fatalf("unexpected SOAP body: %v", body)
	}

	_ = inWriter.Close()
}
