package main

import (
	"encoding/json"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

func one(f string) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	data, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	var desc v2.Descriptor
	if err := json.Unmarshal(data, &desc); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	rt, err := descruntime.ConvertFromV2(&desc)
	if err != nil {
		return nil, fmt.Errorf("convert: %w", err)
	}
	return normalisation.Normalise(rt, v1.Algorithm)
}

func main() {
	suffix := os.Getenv("SUFFIX")
	if suffix == "" {
		suffix = ".pr"
	}
	for _, f := range os.Args[1:] {
		out, err := one(f)
		if err != nil {
			out = []byte("ERROR: " + err.Error())
		}
		os.WriteFile(f+suffix, out, 0o644)
	}
}
