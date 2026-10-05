package tenantresolve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// contractLiteral is the exact envelope tenantcore serves for a tenant
// identity. tenantcore has a test pinning the same literal for TenantIdentity,
// so a change on either side fails a test on that side.
const contractLiteral = `{"success":true,"data":{"tenant_id":"507f1f77bcf86cd799439011","slug":"acme","name":"Acme","status":"active","domain":"acme.example","hosts":[]}}`

func resolveBody(t *testing.T, body string) (Identity, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(ClientConfig{BaseURL: srv.URL, ServiceKey: testSvcKey})
	t.Cleanup(c.Close)
	return c.Resolve(context.Background(), testKey)
}

func TestContract_FullEnvelopeDecodesEveryField(t *testing.T) {
	id, err := resolveBody(t, contractLiteral)
	if err != nil {
		t.Fatal(err)
	}
	if id.TenantID.Hex() != "507f1f77bcf86cd799439011" || id.Slug != "acme" || id.Name != "Acme" ||
		id.Domain != "acme.example" || id.Suspended || id.Stale {
		t.Fatalf("decoded %+v", id)
	}
	if id.Hosts == nil || len(id.Hosts) != 0 {
		t.Fatalf("Hosts = %#v, want empty non-nil", id.Hosts)
	}
}

func TestContract_NullHostsDecodes(t *testing.T) {
	id, err := resolveBody(t, `{"success":true,"data":{"tenant_id":"507f1f77bcf86cd799439011","slug":"acme","name":"Acme","status":"active","domain":"acme.example","hosts":null}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(id.Hosts) != 0 || id.Slug != "acme" || id.Suspended {
		t.Fatalf("decoded %+v", id)
	}
}

// Anything but "active" is suspended: an empty or unknown status must not
// fail open.
func TestStatusMapping_OnlyActiveIsNotSuspended(t *testing.T) {
	for status, wantSuspended := range map[string]bool{
		"active": false, "suspended": true, "": true, "frozen": true,
	} {
		id, err := resolveBody(t, `{"success":true,"data":{"tenant_id":"507f1f77bcf86cd799439011","status":"`+status+`"}}`)
		if err != nil {
			t.Fatalf("%q: %v", status, err)
		}
		if id.Suspended != wantSuspended {
			t.Errorf("status %q: Suspended = %v, want %v", status, id.Suspended, wantSuspended)
		}
	}
}
