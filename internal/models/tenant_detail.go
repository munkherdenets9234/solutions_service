package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ProjectMetric is one highlighted stat on a project's "Our Projects"
// case-study detail page (e.g. "40% faster checkout"). Label is a locale
// map — see internal/i18n.
type ProjectMetric struct {
	Label map[string]string `bson:"label" json:"label"`
	Value string            `bson:"value" json:"value"`
}

// TenantDetail is a tenant's case-study/showcase content for the public
// "Our Projects" page — one-to-one with a Tenant (see TenantID), stored in
// its own tenant_details collection rather than embedded on the tenant
// document itself, so the platform's core identity/billing record (status,
// domain, api key) stays lean and those fields can only ever change through
// their own dedicated routes. A tenant only appears on the public showcase
// once Showcase is true (see TenantDetailRepo.FindAllShowcase).
type TenantDetail struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	// Tagline, Description are locale maps — see internal/i18n.
	Tagline     map[string]string `bson:"tagline,omitempty" json:"tagline,omitempty"`
	Description map[string]string `bson:"description,omitempty" json:"description,omitempty"`
	Category    string            `bson:"category,omitempty" json:"category,omitempty"`
	// WebsiteURL is the project's own live site — shown as "visit website" on
	// the case-study card/detail page. Independent of the tenant's own Domain
	// (used for API-key CORS binding, see TenantMiddleware / PUT
	// /platform/tenants/{id}/domain): the two don't always match, e.g. a
	// showcased client site can differ from whatever domain the tenant's API
	// key is locked to, or the tenant may have no domain lock set at all.
	WebsiteURL string `bson:"website_url,omitempty" json:"website_url,omitempty"`
	CoverImage Image  `bson:"cover_image,omitempty" json:"cover_image,omitempty"`
	// AdminCover is a separate image the admin curates specifically for
	// front-page/list display (e.g. a homepage grid or logo strip) — same
	// shape as CoverImage but chosen independently, since the best image for
	// a compact front-page card isn't always the same as the full
	// case-study detail page's hero banner.
	AdminCover Image           `bson:"admin_cover,omitempty" json:"admin_cover,omitempty"`
	Images     []Image         `bson:"images,omitempty" json:"images,omitempty"`
	Metrics    []ProjectMetric `bson:"metrics,omitempty" json:"metrics,omitempty"`
	Showcase   bool            `bson:"showcase" json:"showcase"`
	Featured   bool            `bson:"featured" json:"featured"`
	SortOrder  int             `bson:"sort_order,omitempty" json:"sort_order,omitempty"`
	CreatedAt  time.Time       `bson:"created_at" json:"created_at"`
	UpdatedAt  time.Time       `bson:"updated_at" json:"updated_at"`
	// UserID is the platform_users._id of the superadmin who last
	// created/updated this record. Nil if never touched.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read — not persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
}
