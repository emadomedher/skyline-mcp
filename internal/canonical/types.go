package canonical

// Service is a canonical representation of an external API.
type Service struct {
	Name       string
	BaseURL    string
	Operations []*Operation
}

// Operation is a canonical operation derived from a spec.
type Operation struct {
	ServiceName       string
	ID                string
	ToolName          string
	Method            string // HTTP method (GET, POST, etc.)
	HTTPMethod        string // Alias for Method (for clarity)
	Path              string
	Summary           string
	Description       string // Detailed description
	Parameters        []Parameter
	RequestBody       *RequestBody
	InputSchema       map[string]any
	ResponseSchema    map[string]any
	StaticHeaders     map[string]string
	SoapNamespace     string
	DynamicURLParam   string
	QueryParamsObject string
	RequiresCrumb     bool
	ContentType       string // Content-Type header
	GraphQL           *GraphQLOperation
	JSONRPC           *JSONRPCOperation
	Protocol          string // "http" (default) or "grpc"
	GRPCMeta          *GRPCOperationMeta
}

// Parameter describes an operation input parameter.
type Parameter struct {
	Name     string
	In       string // path, query, header
	Required bool
	Schema   map[string]any
}

// RequestBody describes a JSON request body.
type RequestBody struct {
	Required    bool
	ContentType string
	Schema      map[string]any
	Content     map[string]MediaType // OpenAPI-style content types
}

// MediaType describes a media type schema
type MediaType struct {
	Schema map[string]any
}

type GraphQLOperation struct {
	OperationType     string
	FieldName         string
	ArgTypes          map[string]string
	DefaultSelection  string
	RequiresSelection bool
	// Composite operation support (CRUD grouping)
	Composite         *GraphQLComposite
}

// GraphQLComposite holds metadata for composite CRUD operations
type GraphQLComposite struct {
	Pattern  string             // Base type name (e.g., "Issue")
	Create   *GraphQLOpRef      // createIssue operation
	Update   *GraphQLOpRef      // updateIssue operation
	Delete   *GraphQLOpRef      // deleteIssue operation
	SetOps   []*GraphQLOpRef    // issueSetLabels, etc.
}

// GraphQLOpRef references a GraphQL operation
type GraphQLOpRef struct {
	Name string // Operation name (e.g., "createIssue")
	Type string // "query" or "mutation"
}

type JSONRPCOperation struct {
	MethodName string
}

type GRPCOperationMeta struct {
	ServiceFullName string
	MethodName      string
	InputFields     []GRPCField
}

type GRPCField struct {
	Name     string
	JSONType string // "string", "number", "integer", "boolean", "object", "array"
	Repeated bool
}
