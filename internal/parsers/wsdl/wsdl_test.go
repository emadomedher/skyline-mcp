package wsdl

import (
	"context"
	"testing"
)

func TestParseToCanonical(t *testing.T) {
	wsdlDoc := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:tns="http://example.com/tns"
  targetNamespace="http://example.com/tns">
  <service name="TestService">
    <port name="TestPort" binding="tns:TestBinding">
      <soap:address location="http://example.com/soap" />
    </port>
  </service>
  <binding name="TestBinding" type="tns:TestPortType">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http" />
    <operation name="Echo">
      <soap:operation soapAction="urn:Echo" />
      <input><soap:body use="literal" /></input>
      <output><soap:body use="literal" /></output>
    </operation>
  </binding>
</definitions>`)

	service, err := ParseToCanonical(context.Background(), wsdlDoc, "api", "")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if service.BaseURL != "http://example.com/soap" {
		t.Fatalf("unexpected base URL: %s", service.BaseURL)
	}
	if len(service.Operations) != 1 {
		t.Fatalf("expected 1 operation")
	}
	op := service.Operations[0]
	if op.ToolName != "api__Echo" {
		t.Fatalf("unexpected tool name: %s", op.ToolName)
	}
	if op.RequestBody == nil || op.RequestBody.ContentType != "text/xml; charset=utf-8" {
		t.Fatalf("unexpected content type")
	}
	if op.StaticHeaders["SOAPAction"] != "urn:Echo" {
		t.Fatalf("missing SOAPAction")
	}
}
