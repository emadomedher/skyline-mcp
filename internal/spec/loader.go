package spec

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
	graphqlparser "skyline-mcp/internal/parsers/graphql"
	grpcparser "skyline-mcp/internal/parsers/grpc"
	"skyline-mcp/internal/providers"
	"skyline-mcp/internal/redact"
)

func LoadServices(ctx context.Context, cfg *config.Config, logger *log.Logger, redactor *redact.Redactor) ([]*canonical.Service, error) {
	fetcher := NewFetcher(15 * time.Second)
	adapters := []SpecAdapter{
		NewOpenAPIAdapter(),
		NewSwagger2Adapter(),
		NewAsyncAPIAdapter(),
		NewPostmanAdapter(),
		NewInsomniaAdapter(),
		NewGoogleDiscoveryAdapter(),
		NewOpenRPCAdapter(),
		NewGraphQLAdapter(),
		NewJenkinsAdapter(),
		NewWSDLAdapter(),
		NewODataAdapter(),
		NewRAMLAdapter(),
		NewAPIBlueprintAdapter(),
		NewCKANAdapter(),
	}

	var services []*canonical.Service
	for i, api := range cfg.APIs {
		svc, err := loadSingleAPI(ctx, fetcher, adapters, api, i, logger, redactor)
		if err != nil {
			logger.Printf("WARNING: skipping api %q (apis[%d]): %v", api.Name, i, err)
			continue
		}
		services = append(services, svc)
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("all %d APIs failed to load", len(cfg.APIs))
	}

	// Apply built-in provider-specific overrides (before user filters)
	services = providers.ApplyProviderOverrides(services, cfg.APIs, logger)

	// Apply operation filters (user-configured)
	services = ApplyOperationFilters(services, cfg.APIs)

	// Apply REST CRUD grouping to reduce tool count
	services = ApplyRESTGrouping(services, cfg.APIs, logger)

	return services, nil
}

func loadSingleAPI(ctx context.Context, fetcher *Fetcher, adapters []SpecAdapter, api config.APIConfig, idx int, logger *log.Logger, redactor *redact.Redactor) (*canonical.Service, error) {
	// Special path for gRPC: use reflection instead of file-based spec.
	if api.SpecType == "grpc" {
		target := strings.TrimPrefix(strings.TrimPrefix(api.BaseURLOverride, "http://"), "https://")
		logger.Printf("loading grpc service %s via reflection from %s", api.Name, target)
		svc, err := grpcparser.ParseViaReflection(ctx, target, api.Name)
		if err != nil {
			return nil, fmt.Errorf("grpc reflection: %w", err)
		}
		return svc, nil
	}

	// If spec_type is set to a known adapter, use it directly without fetching.
	if api.SpecType != "" {
		for _, adapter := range adapters {
			if adapter.Name() == api.SpecType {
				logger.Printf("using adapter %s directly for %s (spec_type override)", api.SpecType, api.Name)
				return adapter.Parse(ctx, nil, api.Name, api.BaseURLOverride)
			}
		}
	}

	var raw []byte
	var err error

	if api.SpecFile != "" {
		logger.Printf("loading spec for %s from file %s", api.Name, api.SpecFile)
		raw, err = os.ReadFile(api.SpecFile)
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
	} else {
		specURL := api.SpecURL
		fetchAuth := api.Auth // auth to use for spec fetch; nil for well-known public URLs
		if specURL == "" || looksLikeDeadSlackSpec(specURL) {
			if slackURL, ok := resolveSlackSpecURL(api); ok {
				logger.Printf("using well-known slack spec for %s", api.Name)
				specURL = slackURL
				fetchAuth = nil
			}
		}
		if looksLikeGitLabSpec(specURL, api) {
			if looksLikeGraphQLEndpoint(specURL) {
				// GitLab GraphQL: fetch introspection from public gitlab.com
				// (self-hosted instances often block introspection or return 500)
				logger.Printf("fetching well-known gitlab graphql schema for %s via public introspection", api.Name)
				raw, err = fetcher.FetchGraphQLIntrospection(ctx, gitlabGraphQLIntrospectionURL, nil)
				if err != nil {
					return nil, fmt.Errorf("gitlab graphql introspection: %w", err)
				}
			} else {
				logger.Printf("using well-known gitlab spec for %s", api.Name)
				specURL = gitlabSpecURL
				fetchAuth = nil
			}
		}
		if looksLikeJiraBase(specURL) {
			if jiraSpecURL, ok := detectJiraSpecURL(ctx, fetcher, specURL, api.Auth); ok {
				logger.Printf("detected jira cloud for %s; using %s", api.Name, redactor.Redact(jiraSpecURL))
				specURL = jiraSpecURL
				fetchAuth = nil
			}
		}
		if looksLikeGmailAPI(api) {
			logger.Printf("using well-known gmail discovery spec for %s", api.Name)
			specURL = gmailDiscoveryURL
			fetchAuth = nil
		}
		if raw == nil {
			logger.Printf("loading spec for %s from %s", api.Name, redactor.Redact(specURL))
			raw, err = fetcher.Fetch(ctx, specURL, fetchAuth)
			logger.Printf("fetch completed for %s (len=%d, err=%v)", api.Name, len(raw), err)
			if err != nil {
				if looksLikeGraphQLEndpoint(specURL) {
					logger.Printf("fetching graphql introspection for %s from %s", api.Name, redactor.Redact(specURL))
					raw, err = fetcher.FetchGraphQLIntrospection(ctx, specURL, api.Auth)
				}
				if err != nil {
					return nil, fmt.Errorf("fetch spec: %w", err)
				}
			}
		}
	}

	parseRaw := func(raw []byte) (*canonical.Service, string, error) {
		for _, adapter := range adapters {
			logger.Printf("trying adapter: %s", adapter.Name())
			if !adapter.Detect(raw) {
				continue
			}

			// Add GraphQL optimization to context if this is a GraphQL API
			parseCtx := ctx
			if adapter.Name() == "graphql" {
				opt := api.Optimization
				// Auto-enable CRUD grouping for GitLab GraphQL to reduce 730 ops to manageable tools
				if opt == nil && isGitLabAPI(api) {
					opt = &config.GraphQLOptimization{EnableCRUDGrouping: true}
				}
				if opt != nil {
					parseCtx = graphqlparser.SetOptimizationInContext(ctx, opt)
				}
			}

			parsed, err := adapter.Parse(parseCtx, raw, api.Name, api.BaseURLOverride)
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
		return nil, fmt.Errorf("parse: %w", err)
	}
	if api.SpecFile == "" && looksLikeGraphQLEndpoint(api.SpecURL) {
		if service == nil || adapterName != "graphql" {
			logger.Printf("retrying %s with graphql introspection from %s", api.Name, redactor.Redact(api.SpecURL))
			raw, err = fetcher.FetchGraphQLIntrospection(ctx, api.SpecURL, api.Auth)
			if err != nil {
				return nil, fmt.Errorf("graphql introspection: %w", err)
			}
			service, adapterName, err = parseRaw(raw)
			if err != nil {
				return nil, fmt.Errorf("graphql parse: %w", err)
			}
		}
	}
	if service == nil {
		return nil, fmt.Errorf("no supported spec detected")
	}
	if api.Jenkins != nil && adapterName != "jenkins" {
		return nil, fmt.Errorf("jenkins config provided but spec is %s", adapterName)
	}
	if api.Jenkins != nil && len(api.Jenkins.AllowWrites) > 0 {
		if err := appendJenkinsWrites(service, api); err != nil {
			return nil, fmt.Errorf("jenkins writes: %w", err)
		}
	}
	return service, nil
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

const slackSpecURL = "https://raw.githubusercontent.com/slackapi/slack-api-specs/master/web-api/slack_web_openapi_v2.json"
const gitlabSpecURL = "https://gitlab.com/gitlab-org/gitlab/-/raw/master/doc/api/openapi/openapi_v2.yaml"
const gitlabGraphQLIntrospectionURL = "https://gitlab.com/api/graphql"
const gmailDiscoveryURL = "https://gmail.googleapis.com/$discovery/rest?version=v1"

// ResolveWellKnownSpecURL resolves well-known API spec URLs that need special handling.
// Returns the resolved URL (or the original if no resolution needed).
// This handles Slack dead URLs, GitLab, and Jira base URLs without needing auth.
func ResolveWellKnownSpecURL(name, specURL string) string {
	nameL := strings.ToLower(name)

	// Slack: dead api.slack.com/specs URL or name-based detection
	if specURL == "" || looksLikeDeadSlackSpec(specURL) {
		if strings.Contains(nameL, "slack") {
			return slackSpecURL
		}
	}

	// GitLab: resolve to well-known spec
	if strings.Contains(nameL, "gitlab") {
		if !looksLikeGraphQLEndpoint(specURL) {
			return gitlabSpecURL
		}
	}

	// Jira: base URL → public spec
	if looksLikeJiraBase(specURL) {
		return "https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json"
	}

	// Gmail: resolve to discovery document
	if strings.Contains(nameL, "gmail") {
		return gmailDiscoveryURL
	}

	return specURL
}

func looksLikeGmailAPI(api config.APIConfig) bool {
	nameL := strings.ToLower(api.Name)
	if strings.Contains(nameL, "gmail") {
		return true
	}
	specL := strings.ToLower(api.SpecURL)
	return strings.Contains(specL, "gmail.googleapis.com")
}

func looksLikeDeadSlackSpec(specURL string) bool {
	lower := strings.ToLower(specURL)
	return strings.Contains(lower, "api.slack.com/specs")
}

func resolveSlackSpecURL(api config.APIConfig) (string, bool) {
	nameL := strings.ToLower(api.Name)
	if strings.Contains(nameL, "slack") || api.SpecType == "slack" {
		return slackSpecURL, true
	}
	return "", false
}

// looksLikeGitLabSpec returns true if the spec URL points at a GitLab instance's
// openapi.json (which often requires sign-in) or if the API name suggests GitLab.
func looksLikeGitLabSpec(specURL string, api config.APIConfig) bool {
	nameL := strings.ToLower(api.Name)
	if strings.Contains(nameL, "gitlab") || api.SpecType == "gitlab" {
		return true
	}
	if specURL == "" {
		return false
	}
	lower := strings.ToLower(specURL)
	return strings.Contains(lower, "/api/openapi.json") && !strings.Contains(lower, "gitlab.com/gitlab-org")
}

func isGitLabAPI(api config.APIConfig) bool {
	return strings.Contains(strings.ToLower(api.Name), "gitlab") || api.SpecType == "gitlab"
}

func detectJiraSpecURL(ctx context.Context, fetcher *Fetcher, baseURL string, auth *config.AuthConfig) (string, bool) {
	baseURL = strings.TrimRight(baseURL, "/")
	serverInfoURL := baseURL + "/rest/api/3/serverInfo"
	if _, err := fetcher.Fetch(ctx, serverInfoURL, auth); err != nil {
		return "", false
	}
	return "https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json", true
}
