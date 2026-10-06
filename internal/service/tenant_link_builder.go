package service

import (
	"context"
	"errors"
	"strings"

	"github.com/eandstravel/digitalservice/internal/tenantresolve"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// identityLookup is the last-known tenant identity, from the tenantresolve
// client's own cache. It never makes a network call.
type identityLookup interface {
	IdentityFor(tenantID primitive.ObjectID) (tenantresolve.Identity, bool)
}

// adminPathSegments is the admin console section for each kind of request.
var adminPathSegments = map[NotifyKind]string{
	NotifyBooking:  "bookings",
	NotifyRental:   "rentals",
	NotifyTransfer: "airport-transfers",
	NotifyGuide:    "guide-applications",
}

// TenantLinkBuilder builds the links a request email carries: the admin page of
// the record and the tenant's public site base. Both are always https, because
// tenantcore refuses any other scheme.
type TenantLinkBuilder struct {
	ids identityLookup
}

func NewTenantLinkBuilder(ids identityLookup) *TenantLinkBuilder {
	return &TenantLinkBuilder{ids: ids}
}

// Links returns an error (so the notifier skips the mail) when the tenant is
// not in the identity cache, is suspended, or has no usable host. The errors
// carry no tenant data.
func (b *TenantLinkBuilder) Links(_ context.Context, tenantID primitive.ObjectID, kind NotifyKind, recordID string) (adminURL, siteBase, tenantName string, err error) {
	if b == nil || b.ids == nil {
		return "", "", "", errors.New("link builder: no identity source")
	}
	seg, ok := adminPathSegments[kind]
	if !ok {
		return "", "", "", errors.New("link builder: unknown request kind")
	}
	ident, ok := b.ids.IdentityFor(tenantID)
	if !ok || ident.TenantID != tenantID {
		return "", "", "", errors.New("link builder: tenant identity not cached")
	}
	if ident.Suspended {
		return "", "", "", errors.New("link builder: tenant suspended")
	}
	if len(ident.Hosts) == 0 {
		return "", "", "", errors.New("link builder: tenant has no host")
	}
	host := strings.TrimRight(strings.TrimSpace(ident.Hosts[0]), "/")
	if host == "" || strings.ContainsAny(host, " \t\r\n/?#@\\") {
		return "", "", "", errors.New("link builder: tenant host is not usable")
	}
	siteBase = "https://" + host
	return siteBase + "/admin/" + seg + "/" + recordID, siteBase, ident.Name, nil
}
