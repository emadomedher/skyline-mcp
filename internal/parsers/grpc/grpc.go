package grpcparser

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"skyline-mcp/internal/canonical"

	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ParseViaReflection connects to a gRPC server, uses reflection to discover
// services and methods, and returns a canonical Service.
func ParseViaReflection(ctx context.Context, target, apiName string) (*canonical.Service, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc: dial %s: %w", target, err)
	}
	defer conn.Close()

	client := grpcreflect.NewClientAuto(ctx, conn)
	defer client.Reset()

	serviceNames, err := client.ListServices()
	if err != nil {
		return nil, fmt.Errorf("grpc: list services: %w", err)
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: target,
	}

	for _, svcName := range serviceNames {
		// Skip internal gRPC services.
		if strings.HasPrefix(svcName, "grpc.reflection.") || strings.HasPrefix(svcName, "grpc.health.") {
			continue
		}

		svcDesc, err := client.ResolveService(svcName)
		if err != nil {
			continue
		}

		methods := svcDesc.GetMethods()
		for _, method := range methods {
			if method.IsClientStreaming() || method.IsServerStreaming() {
				continue // Only support unary RPCs for now.
			}

			op := buildGRPCOperation(apiName, svcName, method.GetName(), method.GetInputType().UnwrapMessage())
			service.Operations = append(service.Operations, op)
		}
	}

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("grpc: no unary methods found on %s", target)
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

func buildGRPCOperation(apiName, serviceName, methodName string, inputMsg protoreflect.MessageDescriptor) *canonical.Operation {
	// Build a short service prefix from the service name (last segment).
	parts := strings.Split(serviceName, ".")
	shortSvc := parts[len(parts)-1]

	operationID := shortSvc + "_" + methodName
	toolName := canonical.ToolName(apiName, operationID)
	summary := fmt.Sprintf("%s.%s", serviceName, methodName)

	var fields []canonical.GRPCField
	properties := map[string]any{}
	requiredFields := []string{}

	if inputMsg != nil {
		for i := 0; i < inputMsg.Fields().Len(); i++ {
			fd := inputMsg.Fields().Get(i)
			jsonType := protoKindToJSONType(fd.Kind())
			field := canonical.GRPCField{
				Name:     string(fd.Name()),
				JSONType: jsonType,
				Repeated: fd.IsList(),
			}
			fields = append(fields, field)

			schema := map[string]any{"type": jsonType}
			if fd.IsList() {
				schema = map[string]any{
					"type":  "array",
					"items": map[string]any{"type": jsonType},
				}
			}
			properties[string(fd.Name())] = schema
		}
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":          properties,
		"additionalProperties": false,
	}
	if len(requiredFields) > 0 {
		inputSchema["required"] = requiredFields
	}

	return &canonical.Operation{
		ServiceName: apiName,
		ID:          operationID,
		ToolName:    toolName,
		Method:      "post",
		Path:        fmt.Sprintf("/%s/%s", serviceName, methodName),
		Summary:     summary,
		InputSchema: inputSchema,
		Protocol:    "grpc",
		GRPCMeta: &canonical.GRPCOperationMeta{
			ServiceFullName: serviceName,
			MethodName:      methodName,
			InputFields:     fields,
		},
	}
}

func protoKindToJSONType(k protoreflect.Kind) string {
	switch k {
	case protoreflect.BoolKind:
		return "boolean"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Uint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind,
		protoreflect.Sfixed64Kind, protoreflect.Fixed64Kind:
		return "integer"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "number"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "string"
	case protoreflect.EnumKind:
		return "string"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return "object"
	default:
		return "string"
	}
}
