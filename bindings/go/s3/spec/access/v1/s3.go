package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// S3 is the access type specification for S3 resources.
//
// TODO: replace the sample fields below with the real parameters.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type S3 struct {
	// +ocm:jsonschema-gen:enum=S3/v1,s3/v1
	// +ocm:jsonschema-gen:enum:deprecated=S3,s3
	Type runtime.Type `json:"type"`

	// URL is a sample field. Replace it with the real parameters.
	URL string `json:"url"`
}

func (t *S3) String() string {
	return t.URL
}
