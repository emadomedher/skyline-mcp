package grpcparser

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	v1reflectiongrpc "google.golang.org/grpc/reflection/grpc_reflection_v1"
	v1alphareflectiongrpc "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// ---------------------------------------------------------------------------
// Helpers: build a synthetic proto service for testing
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

// buildTestFileDescriptor builds a FileDescriptorProto equivalent to:
//
//	syntax = "proto3";
//	package test.v1;
//	message HelloRequest { string name = 1; int32 age = 2; }
//	message HelloReply   { string message = 1; }
//	service Greeter { rpc SayHello(HelloRequest) returns (HelloReply); }
func buildTestFileDescriptor() *descriptorpb.FileDescriptorProto {
	syntax := "proto3"
	pkg := "test.v1"
	fileName := "test/v1/greeter.proto"

	labelOptional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	typeString := descriptorpb.FieldDescriptorProto_TYPE_STRING
	typeInt32 := descriptorpb.FieldDescriptorProto_TYPE_INT32

	nameField := "name"
	nameFieldNum := int32(1)
	ageField := "age"
	ageFieldNum := int32(2)
	messageField := "message"
	messageFieldNum := int32(1)

	reqMsg := &descriptorpb.DescriptorProto{
		Name: strPtr("HelloRequest"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: &nameField, Number: &nameFieldNum, Type: &typeString, Label: &labelOptional},
			{Name: &ageField, Number: &ageFieldNum, Type: &typeInt32, Label: &labelOptional},
		},
	}

	replyMsg := &descriptorpb.DescriptorProto{
		Name: strPtr("HelloReply"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: &messageField, Number: &messageFieldNum, Type: &typeString, Label: &labelOptional},
		},
	}

	inputType := ".test.v1.HelloRequest"
	outputType := ".test.v1.HelloReply"
	methodName := "SayHello"

	svcDesc := &descriptorpb.ServiceDescriptorProto{
		Name: strPtr("Greeter"),
		Method: []*descriptorpb.MethodDescriptorProto{
			{
				Name:       &methodName,
				InputType:  &inputType,
				OutputType: &outputType,
			},
		},
	}

	return &descriptorpb.FileDescriptorProto{
		Name:        &fileName,
		Syntax:      &syntax,
		Package:     &pkg,
		MessageType: []*descriptorpb.DescriptorProto{reqMsg, replyMsg},
		Service:     []*descriptorpb.ServiceDescriptorProto{svcDesc},
	}
}

// startTestGRPCServer creates a gRPC server with a synthetic Greeter service
// registered via reflection. It returns the server address and a cleanup func.
func startTestGRPCServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()

	// Build the file descriptor and register into a local Files registry.
	fdProto := buildTestFileDescriptor()
	localFiles := new(protoregistry.Files)

	fd, err := protodesc.NewFile(fdProto, localFiles)
	if err != nil {
		t.Fatalf("protodesc.NewFile: %v", err)
	}
	if err := localFiles.RegisterFile(fd); err != nil {
		t.Fatalf("RegisterFile: %v", err)
	}

	svcDescriptor := fd.Services().Get(0)
	methodDescriptor := svcDescriptor.Methods().Get(0)

	// Build a grpc.ServiceDesc so grpc-go can route the method.
	grpcSD := grpc.ServiceDesc{
		ServiceName: string(svcDescriptor.FullName()),
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: string(methodDescriptor.Name()),
				Handler: func(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
					in := dynamicpb.NewMessage(methodDescriptor.Input())
					if err := dec(in); err != nil {
						return nil, err
					}
					return dynamicpb.NewMessage(methodDescriptor.Output()), nil
				},
			},
		},
		Streams:  []grpc.StreamDesc{},
		Metadata: string(fd.Path()),
	}

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	srv := grpc.NewServer()
	srv.RegisterService(&grpcSD, &struct{}{})

	// Register reflection with a custom DescriptorResolver so our synthetic
	// file descriptor is discoverable (we can't rely on global registry).
	reflSvr := reflection.NewServerV1(reflection.ServerOptions{
		Services:           srv,
		DescriptorResolver: localFiles,
	})
	v1reflectiongrpc.RegisterServerReflectionServer(srv, reflSvr)
	v1alphareflectiongrpc.RegisterServerReflectionServer(srv, reflection.NewServer(reflection.ServerOptions{
		Services:           srv,
		DescriptorResolver: localFiles,
	}))

	go func() { _ = srv.Serve(lis) }()

	return lis.Addr().String(), func() {
		srv.Stop()
		lis.Close()
	}
}

// ---------------------------------------------------------------------------
// TestParseViaReflection
// ---------------------------------------------------------------------------

func TestParseViaReflection(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc, err := ParseViaReflection(ctx, addr, "myapi")
	if err != nil {
		t.Fatalf("ParseViaReflection returned error: %v", err)
	}

	// --- Service-level checks ---
	if svc.Name != "myapi" {
		t.Errorf("service name = %q; want %q", svc.Name, "myapi")
	}
	if svc.BaseURL != addr {
		t.Errorf("baseURL = %q; want %q", svc.BaseURL, addr)
	}
	if len(svc.Operations) == 0 {
		t.Fatalf("expected at least 1 operation, got 0")
	}

	// Find the SayHello operation.
	var found bool
	for _, op := range svc.Operations {
		if op.GRPCMeta != nil && op.GRPCMeta.MethodName == "SayHello" {
			found = true

			if op.Method != "post" {
				t.Errorf("method = %q; want %q", op.Method, "post")
			}
			if op.Protocol != "grpc" {
				t.Errorf("protocol = %q; want %q", op.Protocol, "grpc")
			}
			if op.Path != "/test.v1.Greeter/SayHello" {
				t.Errorf("path = %q; want %q", op.Path, "/test.v1.Greeter/SayHello")
			}
			if op.GRPCMeta.ServiceFullName != "test.v1.Greeter" {
				t.Errorf("ServiceFullName = %q; want %q", op.GRPCMeta.ServiceFullName, "test.v1.Greeter")
			}
			if op.ID != "Greeter_SayHello" {
				t.Errorf("ID = %q; want %q", op.ID, "Greeter_SayHello")
			}
			wantToolName := "myapi__Greeter_SayHello"
			if op.ToolName != wantToolName {
				t.Errorf("ToolName = %q; want %q", op.ToolName, wantToolName)
			}
			if op.Summary != "test.v1.Greeter.SayHello" {
				t.Errorf("Summary = %q; want %q", op.Summary, "test.v1.Greeter.SayHello")
			}

			// Verify input fields from the HelloRequest message.
			if op.GRPCMeta.InputFields == nil {
				t.Errorf("expected non-nil InputFields")
			} else {
				fieldMap := map[string]string{}
				for _, f := range op.GRPCMeta.InputFields {
					fieldMap[f.Name] = f.JSONType
				}
				if fieldMap["name"] != "string" {
					t.Errorf("field 'name' type = %q; want %q", fieldMap["name"], "string")
				}
				if fieldMap["age"] != "integer" {
					t.Errorf("field 'age' type = %q; want %q", fieldMap["age"], "integer")
				}
			}
		}
	}
	if !found {
		t.Errorf("SayHello operation not found among %d operations", len(svc.Operations))
	}
}

// ---------------------------------------------------------------------------
// TestProtoKindToJSONType
// ---------------------------------------------------------------------------

func TestProtoKindToJSONType(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "boolean"},
		{protoreflect.Int32Kind, "integer"},
		{protoreflect.Sint32Kind, "integer"},
		{protoreflect.Uint32Kind, "integer"},
		{protoreflect.Int64Kind, "integer"},
		{protoreflect.Sint64Kind, "integer"},
		{protoreflect.Uint64Kind, "integer"},
		{protoreflect.Sfixed32Kind, "integer"},
		{protoreflect.Fixed32Kind, "integer"},
		{protoreflect.Sfixed64Kind, "integer"},
		{protoreflect.Fixed64Kind, "integer"},
		{protoreflect.FloatKind, "number"},
		{protoreflect.DoubleKind, "number"},
		{protoreflect.StringKind, "string"},
		{protoreflect.BytesKind, "string"},
		{protoreflect.EnumKind, "string"},
		{protoreflect.MessageKind, "object"},
		{protoreflect.GroupKind, "object"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("kind_%d", int(tt.kind)), func(t *testing.T) {
			got := protoKindToJSONType(tt.kind)
			if got != tt.want {
				t.Errorf("protoKindToJSONType(%v) = %q; want %q", tt.kind, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestBuildGRPCOperation_NilInput
// ---------------------------------------------------------------------------

func TestBuildGRPCOperation_NilInput(t *testing.T) {
	op := buildGRPCOperation("myapi", "foo.bar.Svc", "DoStuff", nil)

	if op == nil {
		t.Fatal("expected non-nil operation")
	}
	if op.Method != "post" {
		t.Errorf("method = %q; want %q", op.Method, "post")
	}
	if op.Protocol != "grpc" {
		t.Errorf("protocol = %q; want %q", op.Protocol, "grpc")
	}
	if op.Path != "/foo.bar.Svc/DoStuff" {
		t.Errorf("path = %q; want %q", op.Path, "/foo.bar.Svc/DoStuff")
	}
	if op.GRPCMeta == nil {
		t.Fatal("expected non-nil GRPCMeta")
	}
	if op.GRPCMeta.ServiceFullName != "foo.bar.Svc" {
		t.Errorf("ServiceFullName = %q; want %q", op.GRPCMeta.ServiceFullName, "foo.bar.Svc")
	}
	if op.GRPCMeta.MethodName != "DoStuff" {
		t.Errorf("MethodName = %q; want %q", op.GRPCMeta.MethodName, "DoStuff")
	}
	if len(op.GRPCMeta.InputFields) != 0 {
		t.Errorf("expected 0 input fields, got %d", len(op.GRPCMeta.InputFields))
	}
	if op.ID != "Svc_DoStuff" {
		t.Errorf("ID = %q; want %q", op.ID, "Svc_DoStuff")
	}
	if op.ToolName != "myapi__Svc_DoStuff" {
		t.Errorf("ToolName = %q; want %q", op.ToolName, "myapi__Svc_DoStuff")
	}

	// Verify InputSchema is well-formed even with nil inputMsg.
	schema := op.InputSchema
	if schema == nil {
		t.Fatal("expected non-nil InputSchema")
	}
	if schema["type"] != "object" {
		t.Errorf("InputSchema type = %v; want %q", schema["type"], "object")
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema properties is %T; want map[string]any", schema["properties"])
	}
	if len(props) != 0 {
		t.Errorf("expected 0 properties, got %d", len(props))
	}
}
