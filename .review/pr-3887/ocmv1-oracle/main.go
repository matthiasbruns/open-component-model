package main

import (
	"fmt"
	"os"

	"ocm.software/ocm/api/ocm/compdesc"
	_ "ocm.software/ocm/api/ocm/compdesc/normalizations/jsonv1"
	_ "ocm.software/ocm/api/ocm/compdesc/versions/v2"
)

// usage: oracle <cd.json>  -> prints jsonNormalisation/v1 bytes
func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	cd, err := compdesc.Decode(data, compdesc.DisableValidation(true))
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(2)
	}
	out, err := compdesc.Normalize(cd, compdesc.JsonNormalisationV1)
	if err != nil {
		fmt.Fprintln(os.Stderr, "normalize:", err)
		os.Exit(2)
	}
	os.Stdout.Write(out)
}
