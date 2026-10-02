package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// SiteLocales are the languages the public site is written in. A value is
// stored per language; a language with no value falls back, on the site, to the
// wording shipped with it for that same language.
var SiteLocales = []string{"en", "mn", "ko"}

// ContentEntry is one override: a dotted path inside a page and its wording per
// language. Entries are a list rather than a map so that no BSON field name
// contains a dot.
//
// Each value is a string, or an array (of strings, or of flat string-field
// objects). An array is one leaf and is never indexed.
//
// Base is an optional per-language snapshot of the shipped wording the editor
// showed when the value was written. The server never derives it, it stores what
// it is sent; the public read skips a value equal to its base.
type ContentEntry struct {
	Path   string         `bson:"path" json:"path"`
	Values map[string]any `bson:"values" json:"values"`
	Base   map[string]any `bson:"base,omitempty" json:"base,omitempty"`
}

// SitePage holds the overrides of one top-level group of the site's wording
// ("hero", "footer", ...) for one tenant.
type SitePage struct {
	ID        primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	TenantID  primitive.ObjectID  `bson:"tenant_id" json:"-"`
	Page      string              `bson:"page" json:"page"`
	Entries   []ContentEntry      `bson:"entries" json:"entries"`
	UpdatedAt time.Time           `bson:"updated_at" json:"updated_at"`
	UserID    *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
}
