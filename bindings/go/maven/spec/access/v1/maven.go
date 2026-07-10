package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Maven is the access type specification for Maven resources.
//
// TODO: replace the sample fields below with the real parameters.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Maven struct {
	// +ocm:jsonschema-gen:enum=Maven/v1,maven/v1
	// +ocm:jsonschema-gen:enum:deprecated=Maven,maven
	Type runtime.Type `json:"type"`

	// URL is a sample field. Replace it with the real parameters.
	URL string `json:"url"`
}

func (t *Maven) String() string {
	return t.URL
}
