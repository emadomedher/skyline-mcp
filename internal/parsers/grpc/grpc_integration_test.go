//go:build integration

package grpcparser

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Integration tests – require a running Mocking Bird gRPC server on
// localhost:50051-50054.  Run with:
//
//   go test ./internal/parsers/grpc/ -tags integration -v -count=1
// ---------------------------------------------------------------------------

func TestParseMockingBirdClothesService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc, err := ParseViaReflection(ctx, "localhost:50051", "clothes")
	if err != nil {
		t.Fatalf("ParseViaReflection: %v", err)
	}

	if svc.Name != "clothes" {
		t.Errorf("service name = %q; want %q", svc.Name, "clothes")
	}

	if len(svc.Operations) < 1 {
		t.Fatalf("expected at least 1 operation, got %d", len(svc.Operations))
	}

	// Find the ListClothes operation.
	var listOp *struct {
		method          string
		protocol        string
		serviceFullName string
		methodName      string
		inputFields     map[string]bool
	}

	for _, op := range svc.Operations {
		if op.GRPCMeta == nil {
			continue
		}
		if op.GRPCMeta.MethodName == "ListClothes" {
			fields := make(map[string]bool)
			for _, f := range op.GRPCMeta.InputFields {
				fields[f.Name] = true
			}
			listOp = &struct {
				method          string
				protocol        string
				serviceFullName string
				methodName      string
				inputFields     map[string]bool
			}{
				method:          op.Method,
				protocol:        op.Protocol,
				serviceFullName: op.GRPCMeta.ServiceFullName,
				methodName:      op.GRPCMeta.MethodName,
				inputFields:     fields,
			}
			break
		}
	}

	if listOp == nil {
		t.Fatalf("ListClothes operation not found among %d operations", len(svc.Operations))
	}

	if listOp.method != "post" {
		t.Errorf("method = %q; want %q", listOp.method, "post")
	}
	if listOp.protocol != "grpc" {
		t.Errorf("protocol = %q; want %q", listOp.protocol, "grpc")
	}
	if !strings.Contains(listOp.serviceFullName, "ClothesService") {
		t.Errorf("ServiceFullName = %q; want it to contain %q", listOp.serviceFullName, "ClothesService")
	}
	if listOp.methodName != "ListClothes" {
		t.Errorf("MethodName = %q; want %q", listOp.methodName, "ListClothes")
	}
	if !listOp.inputFields["limit"] {
		t.Errorf("expected input field %q; fields = %v", "limit", listOp.inputFields)
	}
	if !listOp.inputFields["id"] {
		t.Errorf("expected input field %q; fields = %v", "id", listOp.inputFields)
	}
}

func TestParseMockingBirdMultiplePorts(t *testing.T) {
	ports := []int{50051, 50052, 50053, 50054}

	for _, port := range ports {
		port := port
		t.Run(fmt.Sprintf("port_%d", port), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			target := fmt.Sprintf("localhost:%d", port)
			svc, err := ParseViaReflection(ctx, target, "clothes")
			if err != nil {
				t.Fatalf("ParseViaReflection(%s): %v", target, err)
			}

			if len(svc.Operations) < 1 {
				t.Fatalf("port %d: expected at least 1 operation, got %d", port, len(svc.Operations))
			}

			// Verify that at least one operation references ClothesService.
			var hasClothes bool
			for _, op := range svc.Operations {
				if op.GRPCMeta != nil && strings.Contains(op.GRPCMeta.ServiceFullName, "ClothesService") {
					hasClothes = true
					break
				}
			}
			if !hasClothes {
				t.Errorf("port %d: no operation references ClothesService", port)
			}
		})
	}
}
