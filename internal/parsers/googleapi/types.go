package googleapi

// DiscoveryDoc represents a Google API Discovery document (partial).
type DiscoveryDoc struct {
	Kind        string                         `json:"kind"`
	Name        string                         `json:"name"`
	Version     string                         `json:"version"`
	Title       string                         `json:"title"`
	RootURL     string                         `json:"rootUrl"`
	ServicePath string                         `json:"servicePath"`
	BaseURL     string                         `json:"baseUrl"`
	Resources   map[string]*DiscoveryResource   `json:"resources"`
	Methods     map[string]*DiscoveryMethod     `json:"methods"`
	Parameters  map[string]*DiscoveryParam      `json:"parameters"`
	Schemas     map[string]*DiscoverySchema     `json:"schemas"`
}

type DiscoveryResource struct {
	Resources map[string]*DiscoveryResource `json:"resources"`
	Methods   map[string]*DiscoveryMethod   `json:"methods"`
}

type DiscoveryMethod struct {
	ID          string                  `json:"id"`
	Path        string                  `json:"path"`
	HTTPMethod  string                  `json:"httpMethod"`
	Description string                  `json:"description"`
	Parameters  map[string]*DiscoveryParam `json:"parameters"`
	Request     *SchemaRef              `json:"request"`
	Response    *SchemaRef              `json:"response"`
}

type DiscoveryParam struct {
	Location    string   `json:"location"`
	Type        string   `json:"type"`
	Format      string   `json:"format"`
	Description string   `json:"description"`
	Enum        []string `json:"enum"`
	Required    bool     `json:"required"`
	Repeated    bool     `json:"repeated"`
	Items       map[string]any `json:"items"`
}

type SchemaRef struct {
	Ref         string `json:"$ref"`
	Description string `json:"description"`
}

type DiscoverySchema struct {
	ID                   string                       `json:"id"`
	Ref                  string                       `json:"$ref"`
	Type                 string                       `json:"type"`
	Format               string                       `json:"format"`
	Description          string                       `json:"description"`
	Enum                 []string                     `json:"enum"`
	Properties           map[string]*DiscoverySchema  `json:"properties"`
	Items                *DiscoverySchema             `json:"items"`
	Required             []string                     `json:"required"`
	AdditionalProperties *DiscoverySchema             `json:"additionalProperties"`
	Repeated             bool                         `json:"repeated"`
}
