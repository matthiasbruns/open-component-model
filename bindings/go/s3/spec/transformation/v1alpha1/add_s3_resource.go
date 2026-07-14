package v1alpha1

import (
	fsv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const AddS3ResourceType = "AddS3Resource"

// AddS3Resource is a transformer specification to upload a resource's content to an S3 bucket.
// The target S3 access carried on the resource is produced from the technology-independent
// coordinate at graph-build time, so any source technology can be placed into S3 through this step.
// Spec: AddS3ResourceSpec - the resource (with its S3 access) and the file to upload.
// Output: AddS3ResourceOutput - the resource descriptor after upload, with the access pinned.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type AddS3Resource struct {
	// +ocm:jsonschema-gen:enum=AddS3Resource/v1alpha1
	Type   runtime.Type         `json:"type"`
	ID     string               `json:"id"`
	Spec   *AddS3ResourceSpec   `json:"spec"`
	Output *AddS3ResourceOutput `json:"output,omitempty"`
}

// AddS3ResourceSpec is the input specification for the AddS3Resource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type AddS3ResourceSpec struct {
	// Resource is the target resource descriptor whose S3 access describes where the content is placed.
	Resource *v2.Resource `json:"resource"`
	// File is the file access specification for the content to upload, e.g. from a preceding Get step.
	File fsv1alpha1.File `json:"file"`
}

// AddS3ResourceOutput is the output specification of the AddS3Resource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type AddS3ResourceOutput struct {
	// Resource is the resource descriptor after upload, with the access pinned (e.g. object version).
	Resource *v2.Resource `json:"resource"`
}
