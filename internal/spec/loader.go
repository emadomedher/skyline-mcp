package spec

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/config"
	grpcparser "mcp-api-bridge/internal/parsers/grpc"
	"mcp-api-bridge/internal/redact"
)

func LoadServices(ctx context.Context, cfg *config.Config, logger *log.Logger, redactor *redact.Redactor) ([]*canonical.Service, error) {
	fetcher := NewFetcher(15 * time.Second)
	adapters := []SpecAdapter{
		NewOpenAPIAdapter(),
		NewSwagger2Adapter(),
		NewPostmanAdapter(),
		NewGoogleDiscoveryAdapter(),
		NewOpenRPCAdapter(),
		NewGraphQLAdapter(),
		NewJenkinsAdapter(),
		NewSlackAdapter(),
		NewWSDLAdapter(),
		NewODataAdapter(),
	}

	var services []*canonical.Service
	for i, api := range cfg.APIs {
		// Special path for gRPC: use reflection instead of file-based spec.
		if api.SpecType == "grpc" {
			target := strings.TrimPrefix(strings.TrimPrefix(api.BaseURLOverride, "http://"), "https://")
			logger.Printf("loading grpc service %s via reflection from %s", api.Name, target)
			svc, err := grpcparser.ParseViaReflection(ctx, target, api.Name)
			if err != nil {
				return nil, fmt.Errorf("apis[%d]: %w", i, err)
			}
			services = append(services, svc)
			continue
		}

		var raw []byte
		var err error

		if api.SpecFile != "" {
			logger.Printf("loading spec for %s from file %s", api.Name, api.SpecFile)
			raw, err = os.ReadFile(api.SpecFile)
			if err != nil {
				return nil, fmt.Errorf("apis[%d]: read file: %w", i, err)
			}
		} else {
			specURL := api.SpecURL
			if looksLikeJiraBase(specURL) {
				if jiraSpecURL, ok := detectJiraSpecURL(ctx, fetcher, specURL, api.Auth); ok {
					logger.Printf("detected jira cloud for %s; using %s", api.Name, redactor.Redact(jiraSpecURL))
					specURL = jiraSpecURL
				}
			}
			logger.Printf("loading spec for %s from %s", api.Name, redactor.Redact(specURL))
			raw, err = fetcher.Fetch(ctx, specURL, api.Auth)
			logger.Printf("fetch completed for %s (len=%d, err=%v)", api.Name, len(raw), err)
			if err != nil {
				if looksLikeGraphQLEndpoint(specURL) {
					logger.Printf("fetching graphql introspection for %s from %s", api.Name, redactor.Redact(specURL))
					raw, err = fetcher.FetchGraphQLIntrospection(ctx, specURL, api.Auth)
				}
				if err != nil {
					return nil, fmt.Errorf("apis[%d]: %w", i, err)
				}
			}
		}
		parseRaw := func(raw []byte) (*canonical.Service, string, error) {
			for _, adapter := range adapters {
			logger.Printf("trying adapter: %s", adapter.Name())
				if !adapter.Detect(raw) {
					continue
				}
				parsed, err := adapter.Parse(ctx, raw, api.Name, api.BaseURLOverride)
				if err != nil {
					return nil, "", fmt.Errorf("%s parse: %w", adapter.Name(), err)
				}
				return parsed, adapter.Name(), nil
			}
			return nil, "", nil
		}

		logger.Printf("parsing spec for %s (size=%d bytes)", api.Name, len(raw))
		service, adapterName, err := parseRaw(raw)
		if err != nil {
		logger.Printf("parse completed for %s (adapter=%s, err=%v)", api.Name, adapterName, err)
			return nil, fmt.Errorf("apis[%d]: %w", i, err)
		}
		if api.SpecFile == "" && looksLikeGraphQLEndpoint(api.SpecURL) {
			if service == nil || adapterName != "graphql" {
				logger.Printf("retrying %s with graphql introspection from %s", api.Name, redactor.Redact(api.SpecURL))
				raw, err = fetcher.FetchGraphQLIntrospection(ctx, api.SpecURL, api.Auth)
				if err != nil {
					return nil, fmt.Errorf("apis[%d]: %w", i, err)
				}
				service, adapterName, err = parseRaw(raw)
				if err != nil {
					return nil, fmt.Errorf("apis[%d]: %w", i, err)
				}
			}
		}
		if service == nil {
			return nil, fmt.Errorf("apis[%d]: no supported spec detected", i)
		}
		if api.Jenkins != nil && adapterName != "jenkins" {
			return nil, fmt.Errorf("apis[%d]: jenkins config provided but spec is %s", i, adapterName)
		}
		if api.Jenkins != nil && len(api.Jenkins.AllowWrites) > 0 {
			if err := appendJenkinsWrites(service, api); err != nil {
				return nil, fmt.Errorf("apis[%d]: jenkins writes: %w", i, err)
			}
		}
		services = append(services, service)
	}

	// Apply operation filters
	services = ApplyOperationFilters(services, cfg.APIs)

	return services, nil
}

func looksLikeGraphQLEndpoint(specURL string) bool {
	if specURL == "" {
		return false
	}
	parsed, err := url.Parse(specURL)
	path := specURL
	if err == nil && parsed.Path != "" {
		path = parsed.Path
	}
	path = strings.ToLower(path)
	if strings.HasSuffix(path, ".graphql") || strings.HasSuffix(path, ".gql") || strings.HasSuffix(path, ".sdl") {
		return false
	}
	return strings.Contains(path, "graphql")
}

func looksLikeJiraBase(specURL string) bool {
	if specURL == "" {
		return false
	}
	parsed, err := url.Parse(specURL)
	if err == nil && parsed.Host != "" {
		return strings.HasSuffix(strings.ToLower(parsed.Host), ".atlassian.net")
	}
	return strings.HasSuffix(strings.ToLower(specURL), ".atlassian.net")
}

func detectJiraSpecURL(ctx context.Context, fetcher *Fetcher, baseURL string, auth *config.AuthConfig) (string, bool) {
	baseURL = strings.TrimRight(baseURL, "/")
	serverInfoURL := baseURL + "/rest/api/3/serverInfo"
	if _, err := fetcher.Fetch(ctx, serverInfoURL, auth); err != nil {
		return "", false
	}
	return "https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json", true
}
