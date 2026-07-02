package access

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v2 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

const (
	WgetConsumerType = "wget"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	wget := &v2.Wget{}
	scheme.MustRegisterWithAlias(wget,
		runtime.NewVersionedType(WgetConsumerType, v2.Version),
		runtime.NewUnversionedType(WgetConsumerType),
	)
}
