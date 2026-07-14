package internal

import (
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helm "ocm.software/open-component-model/bindings/go/helm/spec/access"
	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3access "ocm.software/open-component-model/bindings/go/s3/spec/access"
)

var scheme = runtime.NewScheme(runtime.WithAllowUnknown())

func init() {
	scheme.MustRegisterScheme(oci.Scheme)
	scheme.MustRegisterScheme(descriptorv2.Scheme)
	scheme.MustRegisterScheme(helm.Scheme)
	scheme.MustRegisterScheme(repository.Scheme)
	scheme.MustRegisterScheme(s3access.Scheme)
}
