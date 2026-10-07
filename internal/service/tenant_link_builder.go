package service

import (
	"context"
	"errors"
	"strings"

	"github.com/eandstravel/digitalservice/internal/config"
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
// the record (on the admin console's origin) and the public base this service
// serves the unsubscribe page from. Both origins come from configuration
// (ADMIN_BASE_URL, PUBLIC_BASE_URL), are https, and are global: the tenant
// only supplies the display name.
type TenantLinkBuilder struct {
	ids        identityLookup
	publicBase string
	adminBase  string
}

// NewTenantLinkBuilder takes the two validated origins; a trailing slash is
// trimmed. A value that is not an https origin makes Links fail, so no mail is
// built with a wrong link.
func NewTenantLinkBuilder(ids identityLookup, publicBase, adminBase string) *TenantLinkBuilder {
	return &TenantLinkBuilder{ids: ids, publicBase: strings.TrimSpace(publicBase), adminBase: strings.TrimSpace(adminBase)}
}

// Links returns an error (so the notifier skips the mail) when the tenant is
// not in the identity cache or is suspended, or an origin is unusable. The
// errors carry no tenant data.
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
	pub, err := config.NormalizeBaseURL(b.publicBase)
	if err != nil {
		return "", "", "", errors.New("link builder: public base URL is not usable")
	}
	adm, err := config.NormalizeBaseURL(b.adminBase)
	if err != nil {
		return "", "", "", errors.New("link builder: admin base URL is not usable")
	}
	return adm + "/" + seg + "/" + recordID, pub, ident.Name, nil
}
