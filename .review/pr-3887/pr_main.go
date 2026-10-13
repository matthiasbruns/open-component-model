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

func main() {
	for _, f := range os.Args[1:] {
		data, err := os.ReadFile(f)
		if err != nil {
			panic(err)
		}
		var desc v2.Descriptor
		if err := json.Unmarshal(data, &desc); err != nil {
			fmt.Fprintln(os.Stderr, f, "unmarshal:", err)
			os.WriteFile(f+".pr", []byte("ERROR: "+err.Error()), 0o644)
			continue
		}
		rt, err := descruntime.ConvertFromV2(&desc)
		if err != nil {
			os.WriteFile(f+".pr", []byte("ERROR: "+err.Error()), 0o644)
			continue
		}
		out, err := normalisation.Normalise(rt, v1.Algorithm)
		if err != nil {
			out = []byte("ERROR: " + err.Error())
		}
		os.WriteFile(f+".pr", out, 0o644)
	}
}
