package swagger2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"gopkg.in/yaml.v3"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/openapi"
)

func LooksLikeSwagger2(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	return strings.Contains(lower, "\"swagger\"") || strings.Contains(lower, "swagger:")
}

func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	var doc2 openapi2.T
	if err := json.Unmarshal(raw, &doc2); err != nil {
		if err := yaml.Unmarshal(raw, &doc2); err != nil {
			return nil, fmt.Errorf("swagger2: decode failed: %w", err)
		}
	}
	if doc2.Swagger == "" {
		return nil, fmt.Errorf("swagger2: missing swagger version")
	}
	v3, err := openapi2conv.ToV3(&doc2)
	if err != nil {
		return nil, fmt.Errorf("swagger2: convert to v3 failed: %w", err)
	}
	data, err := json.Marshal(v3)
	if err != nil {
		return nil, fmt.Errorf("swagger2: encode v3 failed: %w", err)
	}
	return openapi.ParseToCanonical(ctx, data, apiName, baseURLOverride)
}
