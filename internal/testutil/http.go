package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// Resp wraps an HTTP response with its body already decoded for convenience.
type Resp struct {
	Status int
	Body   map[string]interface{}
	Raw    []byte
}

// ReqOpts customizes a Do call. Zero value is a valid unauthenticated,
// bodyless, tenant-less request.
type ReqOpts struct {
	Token  string      // Authorization: Bearer <Token>, omitted if empty
	APIKey string      // X-API-Key, omitted if empty
	Body   interface{} // JSON-encoded request body, omitted if nil
}

// Do issues method+path against the app's test server and returns the
// decoded JSON response envelope.
func Do(t testing.TB, app *App, method, path string, opts ReqOpts) Resp {
	t.Helper()

	var bodyReader io.Reader
	if opts.Body != nil {
		b, err := json.Marshal(opts.Body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, app.Server.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if opts.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+opts.Token)
	}
	if opts.APIKey != "" {
		req.Header.Set("X-API-Key", opts.APIKey)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	var decoded map[string]interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded) // best-effort; some responses may not be JSON objects
	}

	return Resp{Status: res.StatusCode, Body: decoded, Raw: raw}
}

// Data returns resp.Body["data"] cast to a map, or nil if absent/not a map.
func (r Resp) Data() map[string]interface{} {
	d, _ := r.Body["data"].(map[string]interface{})
	return d
}

// Message returns resp.Body["message"], the error text on failure responses.
func (r Resp) Message() string {
	m, _ := r.Body["message"].(string)
	return m
}
