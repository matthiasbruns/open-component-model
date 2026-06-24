package example

import "ocm.software/open-component-model/bindings/go/runtime"

// TypedValue associates a runtime.Type with an arbitrary string value.
type TypedValue struct {
	Type  runtime.Type
	Value string
}

// NewTypedValue creates a TypedValue with the given type and value.
func NewTypedValue(typ runtime.Type, value string) TypedValue {
	return TypedValue{Type: typ, Value: value}
}
