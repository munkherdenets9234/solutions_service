package handler

import _ "embed"

//go:embed docs/index.html
var DocsHTML []byte

//go:embed docs/openapi.json
var OpenAPISpec []byte
