// Package docs serves the embedded API reference. It is deliberately outside
// both audience trees: it describes the routes, it does not expose any data,
// and it needs neither an API key nor a token to be useful.
package docs

import _ "embed"

//go:embed docs/index.html
var HTML []byte

//go:embed docs/openapi.json
var OpenAPISpec []byte
