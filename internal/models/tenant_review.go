package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TenantReview is a testimonial from one of the platform's own onboarded
// tenants (clients) — e.g. "what our clients say about working with us" on
// the platform operator's own Review page. Distinct from Review, which is a
// tenant's own customers reviewing that tenant's tours/partners.
type TenantReview struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Rate     int                `bson:"rate" json:"rate"` // 1-5
	// Comment is a locale map (e.g. {"en": "...", "mn": "..."}) — see internal/i18n.
	Comment          map[string]string `bson:"comment" json:"comment"`
	CompanyName      string            `bson:"company_name" json:"company_name"`
	CompanyOwnerName string            `bson:"company_owner_name" json:"company_owner_name"`
	IsActive         bool              `bson:"is_active" json:"is_active"`
	CreatedAt        time.Time         `bson:"created_at" json:"created_at"`
	UpdatedAt        time.Time         `bson:"updated_at" json:"updated_at"`
	// UserID is the platform_users._id of the superadmin who last
	// created/updated this record. Nil if never touched.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
}
