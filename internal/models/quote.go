package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type QuoteStatus string

const (
	QuoteNew       QuoteStatus = "new"
	QuoteContacted QuoteStatus = "contacted"
	QuoteQuoted    QuoteStatus = "quoted"
	QuoteClosed    QuoteStatus = "closed"
)

// Quote is a "request a quote" lead — richer than ContactMessage since it
// carries the structured details a sales quote needs (which package,
// budget, timeline), not just a free-form message. A quote is a lead for a
// *potential* tenant, not an existing one: most come in with no tenant
// relationship at all (a prospect inquiring before ever signing up), so
// TenantID is optional — set only when the lead came through an existing
// tenant's own storefront (via its X-API-Key) or was otherwise linked to
// one after the fact.
type Quote struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	// TenantID is nil for a general inquiry with no existing tenant
	// relationship — see the type doc above.
	TenantID    *primitive.ObjectID `bson:"tenant_id,omitempty" json:"tenant_id,omitempty"`
	Name        string              `bson:"name" json:"name"`
	Email       string              `bson:"email" json:"email"`
	Phone       string              `bson:"phone" json:"phone"`
	CompanyName string              `bson:"company_name" json:"company_name"`
	// PackageSlug optionally references the Package the visitor was viewing.
	// Free-form like Review's related_* fields rather than an ObjectID link,
	// since a quote can also be requested with no package selected yet.
	PackageSlug string      `bson:"package_slug" json:"package_slug"`
	Budget      string      `bson:"budget" json:"budget"`
	Timeline    string      `bson:"timeline" json:"timeline"`
	Message     string      `bson:"message" json:"message"`
	Status      QuoteStatus `bson:"status" json:"status"`
	CreatedAt   time.Time   `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time   `bson:"updated_at" json:"updated_at"`
	// UserID is whoever last changed this quote's status — a tenant_users._id
	// if updated via the tenant's own admin panel (PUT /admin/quotes/{id}/status,
	// only possible when TenantID is set), or a platform_users._id if updated
	// via the platform-wide route (PUT /platform/quotes/{id}/status, works
	// for any quote). Nil until someone acts on it — quotes are created by
	// public, unauthenticated visitors.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read — not persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
}
