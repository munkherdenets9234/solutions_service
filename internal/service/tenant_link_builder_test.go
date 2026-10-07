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

const (
	testPublicBase = "https://api.example.com"
	testAdminBase  = "https://admin.example.com"
)

func TestTenantLinkBuilder_AdminPathPerKind(t *testing.T) {
	tid := primitive.NewObjectID()
	rec := primitive.NewObjectID().Hex()
	// Hosts are present but must not influence any link.
	fake := &fakeIdentityLookup{ok: true, ident: tenantresolve.Identity{TenantID: tid, Name: "Acme Travel", Hosts: []string{"acme.example"}}}
	b := NewTenantLinkBuilder(fake, testPublicBase, testAdminBase)
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
		if want := testAdminBase + "/" + seg + "/" + rec; admin != want {
			t.Errorf("%s admin = %q, want %q", kind, admin, want)
		}
		if site != testPublicBase || name != "Acme Travel" {
			t.Errorf("%s site=%q name=%q", kind, site, name)
		}
	}
	if fake.asked != tid {
		t.Error("looked up the wrong tenant")
	}
}

func TestTenantLinkBuilder_EmptyHostsStillBuildsLinks(t *testing.T) {
	tid := primitive.NewObjectID()
	fake := &fakeIdentityLookup{ok: true, ident: tenantresolve.Identity{TenantID: tid, Name: "Acme"}}
	admin, site, name, err := NewTenantLinkBuilder(fake, testPublicBase, testAdminBase).Links(context.Background(), tid, NotifyBooking, "abc")
	if err != nil || admin != testAdminBase+"/bookings/abc" || site != testPublicBase || name != "Acme" {
		t.Fatalf("admin=%q site=%q name=%q err=%v", admin, site, name, err)
	}
}

func TestTenantLinkBuilder_TrailingSlashAndNoAdminSegment(t *testing.T) {
	tid := primitive.NewObjectID()
	fake := &fakeIdentityLookup{ok: true, ident: tenantresolve.Identity{TenantID: tid}}
	admin, site, _, err := NewTenantLinkBuilder(fake, " https://api.example.com/ ", "https://admin.example.com/").Links(context.Background(), tid, NotifyBooking, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if site != "https://api.example.com" || admin != "https://admin.example.com/bookings/abc" {
		t.Errorf("site=%q admin=%q", site, admin)
	}
}

func TestTenantLinkBuilder_Errors(t *testing.T) {
	tid := primitive.NewObjectID()
	cases := map[string]*fakeIdentityLookup{
		"miss":         {ok: false},
		"suspended":    {ok: true, ident: tenantresolve.Identity{TenantID: tid, Suspended: true}},
		"wrong tenant": {ok: true, ident: tenantresolve.Identity{TenantID: primitive.NewObjectID()}},
	}
	for name, fake := range cases {
		if _, _, _, err := NewTenantLinkBuilder(fake, testPublicBase, testAdminBase).Links(context.Background(), tid, NotifyBooking, "abc"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, _, _, err := NewTenantLinkBuilder(nil, testPublicBase, testAdminBase).Links(context.Background(), tid, NotifyBooking, "abc"); err == nil {
		t.Error("nil lookup: want an error")
	}
	ok := &fakeIdentityLookup{ok: true, ident: tenantresolve.Identity{TenantID: tid}}
	if _, _, _, err := NewTenantLinkBuilder(ok, testPublicBase, testAdminBase).Links(context.Background(), tid, NotifyKind("nope"), "abc"); err == nil {
		t.Error("unknown kind: want an error")
	}
	for _, bad := range [][2]string{{"", testAdminBase}, {testPublicBase, ""}, {"http://api.example.com", testAdminBase}, {testPublicBase, "https://admin.example.com/x"}} {
		if _, _, _, err := NewTenantLinkBuilder(ok, bad[0], bad[1]).Links(context.Background(), tid, NotifyBooking, "abc"); err == nil {
			t.Errorf("bad bases %q: want an error", bad)
		}
	}
}
