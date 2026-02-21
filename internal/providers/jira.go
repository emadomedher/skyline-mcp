package providers

import "skyline-mcp/internal/config"

func init() {
	Register(ProviderOverride{
		Provider: "jira-cloud",
		Reason:   "Jira Cloud deprecated search endpoints (HTTP 410); use /search/jql instead",

		MatchName:    []string{"jira"},
		MatchSpecURL: []string{"atlassian.net", "swagger-v3.v3.json"},

		BlockPatterns: []config.OperationPattern{
			{Path: "/rest/api/2/search", Method: "*", Summary: "Deprecated: returns HTTP 410 on Jira Cloud"},
			{Path: "/rest/api/3/search", Method: "*", Summary: "Deprecated: returns HTTP 410 on Jira Cloud"},
		},
	})
}
