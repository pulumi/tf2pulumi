package main

import "encoding/json"

// https://github.com/hashicorp/terraform/blob/c14d6f4241d1eef6b7ee260c8bfb32a9caa03e53/states/statefile/version4.go#L528
type stateV4 struct {
	Version          uint64                   `json:"version"` // modified from original
	TerraformVersion string                   `json:"terraform_version"`
	Serial           uint64                   `json:"serial"`
	Lineage          string                   `json:"lineage"`
	RootOutputs      map[string]outputStateV4 `json:"outputs"`
	Resources        []resourceStateV4        `json:"resources"`
}

// https://github.com/hashicorp/terraform/blob/c14d6f4241d1eef6b7ee260c8bfb32a9caa03e53/states/statefile/version4.go#L553
type resourceStateV4 struct {
	Module         string                  `json:"module,omitempty"`
	Mode           string                  `json:"mode"`
	Type           string                  `json:"type"`
	Name           string                  `json:"name"`
	EachMode       string                  `json:"each,omitempty"`
	ProviderConfig string                  `json:"provider"`
	Instances      []instanceObjectStateV4 `json:"instances"`
}

// https://github.com/hashicorp/terraform/blob/c14d6f4241d1eef6b7ee260c8bfb32a9caa03e53/states/statefile/version4.go#L563
type instanceObjectStateV4 struct {
	IndexKey interface{} `json:"index_key,omitempty"`
	Status   string      `json:"status,omitempty"`
	Deposed  string      `json:"deposed,omitempty"`

	SchemaVersion  uint64            `json:"schema_version"`
	AttributesRaw  json.RawMessage   `json:"attributes,omitempty"`
	AttributesFlat map[string]string `json:"attributes_flat,omitempty"`

	PrivateRaw []byte `json:"private,omitempty"`

	Dependencies []string `json:"dependencies,omitempty"`
	DependsOn    []string `json:"depends_on,omitempty"`
}

// straight from https://github.com/hashicorp/terraform/blob/c14d6f4241d1eef6b7ee260c8bfb32a9caa03e53/states/statefile/version4.go#L547
type outputStateV4 struct {
	ValueRaw     json.RawMessage `json:"value"`
	ValueTypeRaw json.RawMessage `json:"type"`
	Sensitive    bool            `json:"sensitive,omitempty"`
}
