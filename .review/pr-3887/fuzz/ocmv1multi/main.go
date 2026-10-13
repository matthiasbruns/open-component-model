package main

import (
	"fmt"
	"os"

	"ocm.software/ocm/api/ocm/compdesc"
	_ "ocm.software/ocm/api/ocm/compdesc/normalizations/jsonv1"
	_ "ocm.software/ocm/api/ocm/compdesc/versions/v2"
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
	cd, err := compdesc.Decode(data, compdesc.DisableValidation(true))
	if err != nil {
		return nil, fmt.Errorf("DECODE: %w", err)
	}
	return compdesc.Normalize(cd, compdesc.JsonNormalisationV1)
}

func main() {
	for _, f := range os.Args[1:] {
		out, err := one(f)
		if err != nil {
			out = []byte("ERROR: " + err.Error())
		}
		os.WriteFile(f+".v1", out, 0o644)
	}
}
