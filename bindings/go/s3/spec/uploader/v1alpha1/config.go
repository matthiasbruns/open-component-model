package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ConfigType = "uploader.s3.ocm.software"

// Config configures where the transfer's coordinate-based S3 upload places content. It is carried
// as an entry inside the central generic configuration and extracted with [LookupConfig].
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: uploader.s3.ocm.software/v1alpha1
//	    bucket: mirror-bucket
//	    keyPrefix: oci
//	    region: us-east-1
//	    endpoint: http://minio.internal:9000
//	    usePathStyle: true
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=uploader.s3.ocm.software/v1alpha1
	Type runtime.Type `json:"type"`
	// Bucket is the target bucket resources are uploaded to.
	Bucket string `json:"bucket"`
	// KeyPrefix is prepended to the coordinate-derived object key.
	KeyPrefix string `json:"keyPrefix,omitempty"`
	// Region is the target bucket region.
	Region string `json:"region,omitempty"`
	// Endpoint targets an S3-compatible store (MinIO/Ceph/R2). Empty targets AWS.
	Endpoint string `json:"endpoint,omitempty"`
	// UsePathStyle enables path-style addressing for the target.
	UsePathStyle bool `json:"usePathStyle,omitempty"`
}
