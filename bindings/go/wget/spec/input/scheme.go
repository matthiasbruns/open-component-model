package input

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

// Scheme holds the registered wget input specification types.
var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.Wget{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
		runtime.NewVersionedType(v1.LegacyType, v1.Version),
		runtime.NewUnversionedType(v1.LegacyType),
	)
}
