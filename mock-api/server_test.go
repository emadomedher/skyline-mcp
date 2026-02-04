package main

import (
	"strings"
	"testing"
)

func TestParseSOAPRequestRequestSuffix(t *testing.T) {
	xmlPayload := `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
  <soapenv:Body>
    <ListPlantsRequest>
      <id>1</id>
      <name>fern</name>
    </ListPlantsRequest>
  </soapenv:Body>
</soapenv:Envelope>`

	name, params, err := parseSOAPRequest(strings.NewReader(xmlPayload))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if name != "ListPlants" {
		t.Fatalf("unexpected operation: %s", name)
	}
	if params["id"] != "1" || params["name"] != "fern" {
		t.Fatalf("unexpected params: %v", params)
	}
}
