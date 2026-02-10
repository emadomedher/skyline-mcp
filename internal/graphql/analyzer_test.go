package graphql

import (
	"testing"

	gqlparser "github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestDetectCRUDPatterns(t *testing.T) {
	// Sample GraphQL schema with CRUD operations
	schemaSDL := `
type Query {
	issue(id: ID!): Issue
	issues(filter: IssueFilter): [Issue!]!
	project(id: ID!): Project
	projects: [Project!]!
}

type Mutation {
	createIssue(input: CreateIssueInput!): Issue
	updateIssue(id: ID!, input: UpdateIssueInput!): Issue
	deleteIssue(id: ID!): Boolean
	issueSetLabels(id: ID!, labels: [String!]!): Issue
	issueSetAssignees(id: ID!, assignees: [ID!]!): Issue
	
	createProject(input: CreateProjectInput!): Project
	updateProject(id: ID!, input: UpdateProjectInput!): Project
}

type Issue {
	id: ID!
	title: String!
	description: String
}

type Project {
	id: ID!
	name: String!
}

input CreateIssueInput {
	title: String!
	description: String
}

input UpdateIssueInput {
	title: String
	description: String
}

input IssueFilter {
	state: String
}

input CreateProjectInput {
	name: String!
}

input UpdateProjectInput {
	name: String
}
`

	schema, err := gqlparser.LoadSchema(&ast.Source{Input: schemaSDL})
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	analyzer := NewSchemaAnalyzer(schema)
	patterns := analyzer.DetectCRUDPatterns()

	if len(patterns) != 2 {
		t.Errorf("Expected 2 CRUD patterns, got %d", len(patterns))
	}

	// Check Issue pattern
	var issuePattern *CRUDPattern
	for _, p := range patterns {
		if p.BaseType == "Issue" {
			issuePattern = p
			break
		}
	}

	if issuePattern == nil {
		t.Fatal("Issue CRUD pattern not detected")
	}

	if issuePattern.Create == nil || issuePattern.Create.Name != "createIssue" {
		t.Error("createIssue not detected")
	}

	if issuePattern.Update == nil || issuePattern.Update.Name != "updateIssue" {
		t.Error("updateIssue not detected")
	}

	if issuePattern.Delete == nil || issuePattern.Delete.Name != "deleteIssue" {
		t.Error("deleteIssue not detected")
	}

	if len(issuePattern.SetOps) != 2 {
		t.Errorf("Expected 2 set operations, got %d", len(issuePattern.SetOps))
	}

	if issuePattern.QuerySingle == nil || issuePattern.QuerySingle.Name != "issue" {
		t.Error("Singular query 'issue' not detected")
	}

	if issuePattern.QueryList == nil || issuePattern.QueryList.Name != "issues" {
		t.Error("List query 'issues' not detected")
	}
}

func TestGetScalarFields(t *testing.T) {
	schemaSDL := `
type Query {
	dummy: String
}

type Issue {
	id: ID!
	title: String!
	description: String
	createdAt: String
	author: User
	labels: [Label!]!
}

type User {
	id: ID!
	name: String!
}

type Label {
	id: ID!
	name: String!
}
`

	schema, err := gqlparser.LoadSchema(&ast.Source{Input: schemaSDL})
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	analyzer := NewSchemaAnalyzer(schema)
	scalarFields := analyzer.GetScalarFields("Issue")

	expected := []string{"createdAt", "description", "id", "title"}
	if len(scalarFields) != len(expected) {
		t.Errorf("Expected %d scalar fields, got %d", len(expected), len(scalarFields))
	}

	for i, field := range scalarFields {
		if field != expected[i] {
			t.Errorf("Expected field %s, got %s", expected[i], field)
		}
	}
}

func TestFlattenInputObject(t *testing.T) {
	schemaSDL := `
type Query {
	dummy: String
}

type Mutation {
	createIssue(input: CreateIssueInput!): Issue
}

type Issue {
	id: ID!
	title: String!
}

input CreateIssueInput {
	title: String!
	description: String
	assigneeIds: [ID!]
	labelIds: [ID!]
}
`

	schema, err := gqlparser.LoadSchema(&ast.Source{Input: schemaSDL})
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	analyzer := NewSchemaAnalyzer(schema)
	flattened := analyzer.FlattenInputObject("CreateIssueInput")

	if len(flattened) != 4 {
		t.Errorf("Expected 4 flattened fields, got %d", len(flattened))
	}

	if flattened["title"] == nil || !flattened["title"].Type.NonNull {
		t.Error("title field should be required")
	}

	if flattened["description"] == nil || flattened["description"].Type.NonNull {
		t.Error("description field should be optional")
	}
}

func TestGetTypesByCategory(t *testing.T) {
	schemaSDL := `
type Query {
	dummy: String
}

type Issue {
	id: ID!
	state: IssueState!
}

enum IssueState {
	OPEN
	CLOSED
}

input CreateIssueInput {
	title: String!
}

scalar DateTime
`

	schema, err := gqlparser.LoadSchema(&ast.Source{Input: schemaSDL})
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	analyzer := NewSchemaAnalyzer(schema)
	categories := analyzer.GetTypesByCategory()

	if len(categories["object"]) < 1 {
		t.Error("Expected at least 1 object type")
	}

	found := false
	for _, name := range categories["enum"] {
		if name == "IssueState" {
			found = true
			break
		}
	}
	if !found {
		t.Error("IssueState enum not found in categories")
	}
}

func TestExtractBaseType(t *testing.T) {
	tests := []struct {
		name       string
		fieldName  string
		returnType string
		expected   string
	}{
		{
			name:       "create operation",
			fieldName:  "createIssue",
			returnType: "Issue",
			expected:   "Issue",
		},
		{
			name:       "update operation",
			fieldName:  "updateIssue",
			returnType: "Issue",
			expected:   "Issue",
		},
		{
			name:       "set operation",
			fieldName:  "issueSetLabels",
			returnType: "Issue",
			expected:   "Issue",
		},
		{
			name:       "payload type",
			fieldName:  "createIssue",
			returnType: "CreateIssuePayload",
			expected:   "Issue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ast.Schema{Types: map[string]*ast.Definition{}}
			analyzer := NewSchemaAnalyzer(schema)

			returnType := &ast.Type{NamedType: tt.returnType}
			result := analyzer.extractBaseType(tt.fieldName, returnType)

			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
