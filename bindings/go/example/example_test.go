package example_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"ocm.software/open-component-model/bindings/go/example"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestNewTypedValue(t *testing.T) {
	typ := runtime.NewVersionedType("example", "v1")
	v := example.NewTypedValue(typ, "hello")
	assert.Equal(t, typ, v.Type)
	assert.Equal(t, "hello", v.Value)
}
