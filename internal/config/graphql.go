package config

// GraphQLOptimization holds configuration for GraphQL-specific optimizations
type GraphQLOptimization struct {
	EnableCRUDGrouping bool                      `json:"enable_crud_grouping,omitempty" yaml:"enable_crud_grouping,omitempty"`
	FlattenInputs      bool                      `json:"flatten_inputs,omitempty" yaml:"flatten_inputs,omitempty"`
	ResponseMode       string                    `json:"response_mode,omitempty" yaml:"response_mode,omitempty"` // "essential", "full", "auto"
	TypeProfiles       map[string]*TypeProfile   `json:"type_profiles,omitempty" yaml:"type_profiles,omitempty"`
}

// TypeProfile defines behavior for a specific GraphQL type
type TypeProfile struct {
	GroupMutations bool     `json:"group_mutations,omitempty" yaml:"group_mutations,omitempty"`
	ResponseMode   string   `json:"response_mode,omitempty" yaml:"response_mode,omitempty"`
	ResponseFields []string `json:"response_fields,omitempty" yaml:"response_fields,omitempty"`
}

// TypeBasedFilter filters operations based on GraphQL types
type TypeBasedFilter struct {
	IncludeTypes []string `json:"include_types,omitempty" yaml:"include_types,omitempty"`
	ExcludeTypes []string `json:"exclude_types,omitempty" yaml:"exclude_types,omitempty"`
}

// Update OperationFilter to support type-based mode
type OperationFilterEnhanced struct {
	Mode           string            `json:"mode" yaml:"mode"` // "allowlist", "blocklist", "type-based"
	Operations     []OperationPattern `json:"operations,omitempty" yaml:"operations,omitempty"`
	TypeBased      *TypeBasedFilter   `json:"type_based,omitempty" yaml:"type_based,omitempty"`
}
