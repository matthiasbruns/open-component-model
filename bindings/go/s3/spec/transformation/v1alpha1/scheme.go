package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

// AddS3ResourceV1alpha1 is the versioned type of the AddS3Resource transformation.
var AddS3ResourceV1alpha1 = runtime.NewVersionedType(AddS3ResourceType, Version)

// GetS3ResourceV1alpha1 is the versioned type of the GetS3Resource transformation.
var GetS3ResourceV1alpha1 = runtime.NewVersionedType(GetS3ResourceType, Version)

func init() {
	Scheme.MustRegisterWithAlias(&AddS3Resource{}, AddS3ResourceV1alpha1)
	Scheme.MustRegisterWithAlias(&GetS3Resource{}, GetS3ResourceV1alpha1)
}
