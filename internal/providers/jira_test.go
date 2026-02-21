package providers

import (
	"testing"

	"skyline-mcp/internal/canonical"
)

func TestJiraOverrideRegistered(t *testing.T) {
	all := AllOverrides()
	found := false
	for _, o := range all {
		if o.Provider == "jira-cloud" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected jira-cloud override to be registered")
	}
}

func TestJiraDetection(t *testing.T) {
	tests := []struct {
		name    string
		apiName string
		specURL string
		want    bool
	}{
		{"name jira", "jira", "", true},
		{"name Jira Cloud", "Jira Cloud", "", true},
		{"name my-jira", "my-jira", "", true},
		{"url atlassian.net", "some-api", "https://mycompany.atlassian.net", true},
		{"url swagger spec", "some-api", "https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json", true},
		{"no match", "petstore", "http://localhost:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := DetectOverrides(tt.apiName, tt.specURL)
			got := false
			for _, m := range matches {
				if m.Provider == "jira-cloud" {
					got = true
					break
				}
			}
			if got != tt.want {
				t.Errorf("DetectOverrides(%q, %q) jira-cloud match = %v, want %v", tt.apiName, tt.specURL, got, tt.want)
			}
		})
	}
}

func TestJiraBlockPatterns(t *testing.T) {
	overrides := DetectOverrides("jira", "")
	if len(overrides) == 0 {
		t.Fatal("no overrides found for jira")
	}

	patterns := overrides[0].BlockPatterns

	tests := []struct {
		name   string
		op     *canonical.Operation
		blocked bool
	}{
		{
			"GET /rest/api/2/search blocked",
			&canonical.Operation{ID: "searchForIssuesUsingJql", Method: "GET", Path: "/rest/api/2/search"},
			true,
		},
		{
			"POST /rest/api/2/search blocked",
			&canonical.Operation{ID: "searchForIssuesUsingJqlPost", Method: "POST", Path: "/rest/api/2/search"},
			true,
		},
		{
			"GET /rest/api/3/search blocked",
			&canonical.Operation{ID: "searchForIssuesUsingJql", Method: "GET", Path: "/rest/api/3/search"},
			true,
		},
		{
			"POST /rest/api/3/search blocked",
			&canonical.Operation{ID: "searchForIssuesUsingJqlEnhanced", Method: "POST", Path: "/rest/api/3/search"},
			true,
		},
		{
			"POST /rest/api/2/search/jql NOT blocked",
			&canonical.Operation{ID: "searchForIssuesUsingJqlNew", Method: "POST", Path: "/rest/api/2/search/jql"},
			false,
		},
		{
			"GET /rest/api/2/issue NOT blocked",
			&canonical.Operation{ID: "getIssue", Method: "GET", Path: "/rest/api/2/issue/{issueIdOrKey}"},
			false,
		},
		{
			"GET /rest/api/2/myself NOT blocked",
			&canonical.Operation{ID: "getCurrentUser", Method: "GET", Path: "/rest/api/2/myself"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := operationMatchesAny(tt.op, patterns)
			if got != tt.blocked {
				t.Errorf("operationMatchesAny for %s %s = %v, want %v", tt.op.Method, tt.op.Path, got, tt.blocked)
			}
		})
	}
}
