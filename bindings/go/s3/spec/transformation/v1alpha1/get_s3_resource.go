package v1alpha1

import (
	fsv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const GetS3ResourceType = "GetS3Resource"

// GetS3Resource is a transformer specification to download a resource's content from S3 and buffer
// it to a file, so a following step (e.g. AddLocalResource) can store it. The download is dispatched
// by the resource's S3 access.
// Spec: GetS3ResourceSpec - the resource to download and an optional output path.
// Output: GetS3ResourceOutput - the downloaded file and the resource descriptor.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GetS3Resource struct {
	// +ocm:jsonschema-gen:enum=GetS3Resource/v1alpha1
	Type   runtime.Type         `json:"type"`
	ID     string               `json:"id"`
	Spec   *GetS3ResourceSpec   `json:"spec"`
	Output *GetS3ResourceOutput `json:"output,omitempty"`
}

// GetS3ResourceSpec is the input specification for the GetS3Resource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetS3ResourceSpec struct {
	// Resource is the resource descriptor whose S3 access is downloaded.
	Resource *v2.Resource `json:"resource"`
	// OutputPath is where the content is buffered. If empty, a temporary file is used.
	OutputPath string `json:"outputPath,omitempty"`
}

// GetS3ResourceOutput is the output specification of the GetS3Resource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetS3ResourceOutput struct {
	// File is the file access specification for the downloaded content.
	File fsv1alpha1.File `json:"file"`
	// Resource is the resource descriptor.
	Resource *v2.Resource `json:"resource"`
}
