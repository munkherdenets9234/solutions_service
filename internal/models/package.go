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
	CreatedAt   time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time           `bson:"updated_at" json:"updated_at"`
	// UserID is the platform_users._id of the superadmin who last
	// created/updated this record via /platform/packages. Nil if never
	// touched.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read — not persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
}
