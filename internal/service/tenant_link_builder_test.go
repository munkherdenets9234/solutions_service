package service

import (
	"context"
	"testing"

	"github.com/eandstravel/digitalservice/internal/tenantresolve"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakeIdentityLookup struct {
	ident tenantresolve.Identity
	ok    bool
	asked primitive.ObjectID
}

func (f *fakeIdentityLookup) IdentityFor(id primitive.ObjectID) (tenantresolve.Identity, bool) {
	f.asked = id
	return f.ident, f.ok
}

func TestTenantLinkBuilder_AdminPathPerKind(t *testing.T) {
	tid := primitive.NewObjectID()
	rec := primitive.NewObjectID().Hex()
	fake := &fakeIdentityLookup{ok: true, ident: tenantresolve.Identity{TenantID: tid, Name: "Acme Travel", Hosts: []string{"acme.example", "other.example"}}}
	b := NewTenantLinkBuilder(fake)
	cases := map[NotifyKind]string{
		NotifyBooking:  "bookings",
		NotifyRental:   "rentals",
		NotifyTransfer: "airport-transfers",
		NotifyGuide:    "guide-applications",
	}
	for kind, seg := range cases {
		admin, site, name, err := b.Links(context.Background(), tid, kind, rec)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if want := "https://acme.example/admin/" + seg + "/" + rec; admin != want {
			t.Errorf("%s admin = %q, want %q", kind, admin, want)
		}
		if site != "https://acme.example" || name != "Acme Travel" {
			t.Errorf("%s site=%q name=%q", kind, site, name)
		}
	}
	if fake.asked != tid {
		t.Error("looked up the wrong tenant")
	}
}

func TestTenantLinkBuilder_NeverEmitsHTTPOrDoubleSlash(t *testing.T) {
	tid := primitive.NewObjectID()
	fake := &fakeIdentityLookup{ok: true, ident: tenantresolve.Identity{TenantID: tid, Hosts: []string{" acme.example/ "}}}
	admin, site, _, err := NewTenantLinkBuilder(fake).Links(context.Background(), tid, NotifyBooking, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if site != "https://acme.example" || admin != "https://acme.example/admin/bookings/abc" {
		t.Errorf("site=%q admin=%q", site, admin)
	}
}

func TestTenantLinkBuilder_Errors(t *testing.T) {
	tid := primitive.NewObjectID()
	cases := map[string]*fakeIdentityLookup{
		"miss":         {ok: false},
		"no hosts":     {ok: true, ident: tenantresolve.Identity{TenantID: tid}},
		"blank host":   {ok: true, ident: tenantresolve.Identity{TenantID: tid, Hosts: []string{"  "}}},
		"suspended":    {ok: true, ident: tenantresolve.Identity{TenantID: tid, Hosts: []string{"a.example"}, Suspended: true}},
		"wrong tenant": {ok: true, ident: tenantresolve.Identity{TenantID: primitive.NewObjectID(), Hosts: []string{"a.example"}}},
	}
	for name, fake := range cases {
		if _, _, _, err := NewTenantLinkBuilder(fake).Links(context.Background(), tid, NotifyBooking, "abc"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, _, _, err := NewTenantLinkBuilder(nil).Links(context.Background(), tid, NotifyBooking, "abc"); err == nil {
		t.Error("nil lookup: want an error")
	}
	if _, _, _, err := NewTenantLinkBuilder(&fakeIdentityLookup{ok: true, ident: tenantresolve.Identity{TenantID: tid, Hosts: []string{"a.example"}}}).Links(context.Background(), tid, NotifyKind("nope"), "abc"); err == nil {
		t.Error("unknown kind: want an error")
	}
}
