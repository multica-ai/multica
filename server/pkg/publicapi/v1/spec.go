package publicapiv1

import (
	"bytes"
	_ "embed"
)

//go:embed openapi.yaml
var openAPISpec []byte

// OpenAPI returns an isolated copy of the canonical v1 contract document.
func OpenAPI() []byte {
	return bytes.Clone(openAPISpec)
}
