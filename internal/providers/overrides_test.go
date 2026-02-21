package providers

import (
	"log"
	"os"
	"testing"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
)

func TestDetectOverrides_NameMatch(t *testing.T) {
	matches := DetectOverrides("jira", "")
	if len(matches) == 0 {
		t.Fatal("expected Jira override to match API named 'jira'")
	}
	if matches[0].Provider != "jira-cloud" {
		t.Fatalf("expected provider 'jira-cloud', got %q", matches[0].Provider)
	}
}

func TestDetectOverrides_NameSubstring(t *testing.T) {
	for _, name := range []string{"my-jira", "Jira Cloud", "JIRA-PROD"} {
		matches := DetectOverrides(name, "")
		if len(matches) == 0 {
			t.Errorf("expected Jira override to match API named %q", name)
		}
	}
}

func TestDetectOverrides_URLMatch(t *testing.T) {
	matches := DetectOverrides("some-api", "https://mycompany.atlassian.net")
	if len(matches) == 0 {
		t.Fatal("expected Jira override to match atlassian.net URL")
	}
}

func TestDetectOverrides_NoMatch(t *testing.T) {
	matches := DetectOverrides("petstore", "http://localhost:8080/openapi.json")
	if len(matches) != 0 {
		t.Fatalf("expected no overrides for petstore, got %d", len(matches))
	}
}

func TestDetectOverrides_EmptyInputs(t *testing.T) {
	matches := DetectOverrides("", "")
	if len(matches) != 0 {
		t.Fatalf("expected no overrides for empty inputs, got %d", len(matches))
	}
}

func TestPatternMatches_ExactPath(t *testing.T) {
	op := &canonical.Operation{ID: "searchForIssues", Method: "GET", Path: "/rest/api/2/search"}
	pattern := config.OperationPattern{Path: "/rest/api/2/search", Method: "*"}

	if !patternMatches(op, pattern) {
		t.Fatal("expected pattern to match exact path")
	}
}

func TestPatternMatches_WildcardMethod(t *testing.T) {
	for _, method := range []string{"GET", "POST", "DELETE"} {
		op := &canonical.Operation{ID: "test", Method: method, Path: "/rest/api/2/search"}
		pattern := config.OperationPattern{Path: "/rest/api/2/search", Method: "*"}
		if !patternMatches(op, pattern) {
			t.Errorf("expected wildcard method to match %s", method)
		}
	}
}

func TestPatternMatches_NoMatchDifferentPath(t *testing.T) {
	op := &canonical.Operation{ID: "searchJql", Method: "POST", Path: "/rest/api/2/search/jql"}
	pattern := config.OperationPattern{Path: "/rest/api/2/search", Method: "*"}

	if patternMatches(op, pattern) {
		t.Fatal("expected pattern NOT to match /rest/api/2/search/jql")
	}
}

func TestPatternMatches_OperationIDGlob(t *testing.T) {
	op := &canonical.Operation{ID: "getIssue", Method: "GET", Path: "/rest/api/2/issue"}
	pattern := config.OperationPattern{OperationID: "get*"}

	if !patternMatches(op, pattern) {
		t.Fatal("expected glob pattern to match getIssue")
	}
}

func TestApplyProviderOverrides_BlocksKnownBad(t *testing.T) {
	logger := log.New(os.Stderr, "", 0)
	services := []*canonical.Service{{
		Name:    "jira",
		BaseURL: "https://mycompany.atlassian.net",
		Operations: []*canonical.Operation{
			{ID: "searchForIssues", Method: "GET", Path: "/rest/api/2/search"},
			{ID: "searchJql", Method: "POST", Path: "/rest/api/2/search/jql"},
			{ID: "getIssue", Method: "GET", Path: "/rest/api/2/issue/{issueIdOrKey}"},
		},
	}}
	apiConfigs := []config.APIConfig{{
		Name:    "jira",
		SpecURL: "https://mycompany.atlassian.net",
	}}

	result := ApplyProviderOverrides(services, apiConfigs, logger)
	if len(result) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result))
	}
	ops := result[0].Operations
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations after filtering, got %d", len(ops))
	}
	for _, op := range ops {
		if op.Path == "/rest/api/2/search" {
			t.Fatal("deprecated /rest/api/2/search should have been filtered out")
		}
	}
}

func TestApplyProviderOverrides_RespectsOptOut(t *testing.T) {
	logger := log.New(os.Stderr, "", 0)
	services := []*canonical.Service{{
		Name:    "jira",
		BaseURL: "https://mycompany.atlassian.net",
		Operations: []*canonical.Operation{
			{ID: "searchForIssues", Method: "GET", Path: "/rest/api/2/search"},
			{ID: "getIssue", Method: "GET", Path: "/rest/api/2/issue/{issueIdOrKey}"},
		},
	}}
	apiConfigs := []config.APIConfig{{
		Name:                     "jira",
		SpecURL:                  "https://mycompany.atlassian.net",
		DisableProviderOverrides: true,
	}}

	result := ApplyProviderOverrides(services, apiConfigs, logger)
	if len(result[0].Operations) != 2 {
		t.Fatalf("expected all 2 operations kept with opt-out, got %d", len(result[0].Operations))
	}
}

func TestApplyProviderOverrides_NoMatchPassesThrough(t *testing.T) {
	logger := log.New(os.Stderr, "", 0)
	services := []*canonical.Service{{
		Name:    "petstore",
		BaseURL: "http://localhost:8080",
		Operations: []*canonical.Operation{
			{ID: "getPet", Method: "GET", Path: "/pets/{id}"},
			{ID: "listPets", Method: "GET", Path: "/pets"},
		},
	}}
	apiConfigs := []config.APIConfig{{
		Name:    "petstore",
		SpecURL: "http://localhost:8080/openapi.json",
	}}

	result := ApplyProviderOverrides(services, apiConfigs, logger)
	if len(result[0].Operations) != 2 {
		t.Fatalf("expected all 2 operations for non-matching provider, got %d", len(result[0].Operations))
	}
}
