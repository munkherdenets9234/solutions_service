package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PartnerProduct is one product/service a partner offers, embedded in the
// partner document rather than stored as its own collection.
type PartnerProduct struct {
	Name        string `bson:"name" json:"name"`
	Image       string `bson:"image" json:"image"`
	Description string `bson:"description" json:"description"`
}

type Partner struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID    primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Name        string             `bson:"name" json:"name"`
	Slug        string             `bson:"slug" json:"slug"`
	Tag         string             `bson:"tag" json:"tag"`
	Title       string             `bson:"title" json:"title"`
	Description string             `bson:"description" json:"description"`
	Image       string             `bson:"image" json:"image"` // hero image URL
	WebURL      string             `bson:"web_url" json:"web_url"`
	Products    []PartnerProduct   `bson:"products" json:"products"`
	// RelatedReview optionally links a featured testimonial; the public read
	// path can resolve it via GET /reviews/:id.
	RelatedReview *primitive.ObjectID `bson:"related_review,omitempty" json:"related_review,omitempty"`
	IsActive      bool                `bson:"is_active" json:"is_active"`
	CreatedAt     time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time           `bson:"updated_at" json:"updated_at"`
}
