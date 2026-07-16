package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Review is a customer testimonial shown on the storefront. The related_*
// fields are free-form references (a name or an external id) rather than
// ObjectID links - tours and partners have no collections of their own, and
// the public read path needs a displayable value without extra lookups.
type Review struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID        primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	RelatedCustomer string             `bson:"related_customer" json:"related_customer"`
	Star            int                `bson:"star" json:"star"` // 1-5
	// Review is a locale map (e.g. {"en": "...", "mn": "..."}) — see internal/i18n.
	Review         map[string]string `bson:"review" json:"review"`
	RelatedTour    string            `bson:"related_tour" json:"related_tour"`
	RelatedPartner string            `bson:"related_partner" json:"related_partner"`
	CreatedAt      time.Time         `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time         `bson:"updated_at" json:"updated_at"`
}
