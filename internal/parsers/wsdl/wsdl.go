package wsdl

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"skyline-mcp/internal/canonical"
)

// ParseToCanonical parses WSDL 1.1 XML into a canonical Service.
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx
	fmt.Printf("[WSDL] ParseToCanonical called with baseURLOverride=%q\n", baseURLOverride)
	def, err := parseDefinitions(raw)
	if err != nil {
		return nil, err
	}
	if len(def.Services) == 0 {
		return nil, fmt.Errorf("wsdl: no services found")
	}

	bindingMap := map[string]Binding{}
	for _, binding := range def.Bindings {
		bindingMap[binding.Name] = binding
	}

	service := chooseService(def.Services)
	port := choosePort(service.Ports)
	if port.Binding == "" {
		return nil, fmt.Errorf("wsdl: port missing binding")
	}
	bindingName := localName(port.Binding)
	binding, ok := bindingMap[bindingName]
	if !ok {
		return nil, fmt.Errorf("wsdl: binding %s not found", bindingName)
	}

	// WSDL specs define their endpoint explicitly via soap:address, so always use that
	// and ignore base_url_override (which is meant for REST APIs that use path-based routing)
	if port.Address.Location == "" {
		return nil, fmt.Errorf("wsdl: port missing address location")
	}
	baseURL := strings.TrimRight(port.Address.Location, "/")
	if baseURLOverride != "" {
		fmt.Printf("[WSDL] Ignoring baseURLOverride %q, using soap:address: %q\n", baseURLOverride, baseURL)
	} else {
		fmt.Printf("[WSDL] Using soap:address location: %q\n", baseURL)
	}

	contentType := "text/xml; charset=utf-8"
	soapVersion := soapVersionFromBinding(binding)
	if soapVersion == "1.2" {
		contentType = "application/soap+xml; charset=utf-8"
	}

	ops := make([]BindingOperation, len(binding.Operations))
	copy(ops, binding.Operations)
	sort.Slice(ops, func(i, j int) bool { return ops[i].Name < ops[j].Name })

	serviceModel := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}
	for _, op := range ops {
		if op.Name == "" {
			continue
		}
		operationID := op.Name
		toolName := canonical.ToolName(apiName, operationID)
		inputSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"body": map[string]any{
					"type":        "string",
					"description": "Optional raw SOAP XML payload.",
				},
				"parameters": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
					"description":          "Optional key/value parameters used to build the SOAP body.",
				},
			},
			"additionalProperties": false,
		}
		staticHeaders := map[string]string{}
		if op.SoapOperation.SoapAction != "" {
			staticHeaders["SOAPAction"] = op.SoapOperation.SoapAction
		}
		serviceModel.Operations = append(serviceModel.Operations, &canonical.Operation{
			ServiceName:    apiName,
			ID:             operationID,
			ToolName:       toolName,
			Method:         "post",
			Path:           "",
			Summary:        op.Name + " (SOAP). Use arguments.parameters for key/value inputs, or arguments.body for raw XML.",
			Parameters:     nil,
			RequestBody:    &canonical.RequestBody{Required: false, ContentType: contentType, Schema: map[string]any{"type": "string"}},
			InputSchema:    inputSchema,
			ResponseSchema: nil,
			StaticHeaders:  staticHeaders,
			SoapNamespace:  def.TargetNamespace,
		})
	}

	if len(serviceModel.Operations) == 0 {
		return nil, fmt.Errorf("wsdl: no operations found")
	}
	return serviceModel, nil
}

func parseDefinitions(raw []byte) (*Definitions, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.Strict = false
	var def Definitions
	if err := decoder.Decode(&def); err != nil {
		return nil, fmt.Errorf("wsdl: decode failed: %w", err)
	}
	return &def, nil
}

func chooseService(services []Service) Service {
	idx := 0
	for i := 1; i < len(services); i++ {
		if services[i].Name < services[idx].Name {
			idx = i
		}
	}
	return services[idx]
}

func choosePort(ports []Port) Port {
	idx := 0
	for i := 1; i < len(ports); i++ {
		if ports[i].Name < ports[idx].Name {
			idx = i
		}
	}
	return ports[idx]
}

func localName(qname string) string {
	if idx := strings.Index(qname, ":"); idx >= 0 {
		return qname[idx+1:]
	}
	return qname
}

func soapVersionFromBinding(binding Binding) string {
	if binding.SoapBinding.XMLName.Space == soap12NS {
		return "1.2"
	}
	return "1.1"
}

const (
	soap11NS = "http://schemas.xmlsoap.org/wsdl/soap/"
	soap12NS = "http://schemas.xmlsoap.org/wsdl/soap12/"
)

// WSDL model structs.

type Definitions struct {
	XMLName  xml.Name `xml:"definitions"`
	TargetNamespace string `xml:"targetNamespace,attr"`
	Services []Service `xml:"service"`
	Bindings []Binding `xml:"binding"`
}

type Service struct {
	Name  string `xml:"name,attr"`
	Ports []Port `xml:"port"`
}

type Port struct {
	Name    string  `xml:"name,attr"`
	Binding string  `xml:"binding,attr"`
	Address Address `xml:"address"`
}

type Address struct {
	XMLName  xml.Name `xml:"address"`
	Location string   `xml:"location,attr"`
}

type Binding struct {
	Name        string             `xml:"name,attr"`
	Type        string             `xml:"type,attr"`
	SoapBinding SoapBinding         `xml:"binding"`
	Operations  []BindingOperation `xml:"operation"`
}

type SoapBinding struct {
	XMLName   xml.Name `xml:"binding"`
	Style     string   `xml:"style,attr"`
	Transport string   `xml:"transport,attr"`
}

type BindingOperation struct {
	Name          string        `xml:"name,attr"`
	SoapOperation SoapOperation `xml:"operation"`
}

type SoapOperation struct {
	XMLName   xml.Name `xml:"operation"`
	SoapAction string  `xml:"soapAction,attr"`
	Style     string  `xml:"style,attr"`
}
