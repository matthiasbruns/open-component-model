package access

import (
	"strings"

	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// MavenConsumerType is the OCM type name for the Maven access type.
	MavenConsumerType = "Maven"
)

var V1VersionedType = runtime.NewVersionedType(MavenConsumerType, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	spec := &v1.Maven{}

	lowerCaseConsumerType := strings.ToLower(MavenConsumerType)
	scheme.MustRegisterWithAlias(spec,
		V1VersionedType,
		runtime.NewUnversionedType(MavenConsumerType),
		runtime.NewVersionedType(lowerCaseConsumerType, v1.Version),
		runtime.NewUnversionedType(lowerCaseConsumerType),
	)
}
