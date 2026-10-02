package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Package is one pricing tier in the platform operator's own global price
// list — managed centrally via /platform/packages, not per-tenant. Which
// tenants show a given package on their own storefront is a separate
// many-to-many assignment (see TenantPackage). Price of 0 means "not yet
// set" — the storefront shows a placeholder instead of a real number.
type Package struct {
	ID   primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Slug string             `bson:"slug" json:"slug"`
	// Name, Tagline are locale maps — see internal/i18n.
	Name     map[string]string `bson:"name" json:"name"`
	Tagline  map[string]string `bson:"tagline" json:"tagline"`
	Price    float64           `bson:"price" json:"price"`
	Currency string            `bson:"currency" json:"currency"`
	// BillingNote is a locale map for a short suffix shown next to the price
	// (e.g. "/month", "one-time").
	BillingNote map[string]string `bson:"billing_note" json:"billing_note"`
	// Features is a locale map of bullet points shown on the pricing card.
	Features    map[string][]string `bson:"features" json:"features"`
	Highlighted bool                `bson:"highlighted" json:"highlighted"` // "most popular" ribbon
	SortOrder   int                 `bson:"sort_order" json:"sort_order"`
	IsActive    bool                `bson:"is_active" json:"is_active"`

	// ── Entitlement: what subscribing to this package actually grants ─────
	//
	// These three belong to Package's PLAN role — what the platform charges
	// a tenant for — not to its storefront-pricing-card role. Package is
	// doing both jobs today; when it is split into Plan and Package these
	// move to Plan and nothing else here does. They live on the plan rather
	// than on the Subscription so a price change applies to every tenant on
	// that plan at once, instead of needing a sweep.
	//
	// They are `json:"-"` because GET /api/v1/platform/packages is a PUBLIC,
	// unauthenticated read that returns this struct as-is. Serialising them
	// would publish the internal shape of the price list — which modules
	// exist, what the tiers actually cap — to anyone who asks. This is the
	// widen-a-shared-type leak in miniature: the field is fine, the audience
	// is not. Expose them from an authenticated platform route when one
	// needs to edit them.

	// Modules are the products this plan grants. Empty means no module gate
	// is enforced for tenants on this plan — see entitlement.HasModule,
	// which explains why and when that default has to go.
	Modules []string `bson:"modules,omitempty" json:"-"`

	// Limits are numeric ceilings, keyed by a name the PRODUCT defines. The
	// platform stores the number and does not know what it counts; that is
	// what keeps a new car wash feature from requiring a billing change.
	// Absent key means unlimited, not zero.
	Limits map[string]int `bson:"limits,omitempty" json:"-"`

	// Capabilities are on/off grants, keyed the same way. Absent is off.
	//
	// It is not called Features because Features above is already taken — by
	// the locale map of marketing bullet points printed on the pricing card.
	// That collision is not an accident of naming, it is the two-jobs problem
	// showing through: one of these is prose a visitor reads, the other is a
	// switch the server enforces, and they ended up in one struct because
	// Package is both a plan and an advert. At the Plan/Package split this
	// one goes to Plan and can take the better name back.
	Capabilities map[string]bool `bson:"capabilities,omitempty" json:"-"`
	CreatedAt    time.Time       `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time       `bson:"updated_at" json:"updated_at"`
	// UserID is the platform_users._id of the superadmin who last
	// created/updated this record via /platform/packages. Nil if never
	// touched.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read — not persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
}
