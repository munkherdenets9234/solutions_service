package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Car struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID       primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Slug           string             `bson:"slug" json:"slug"`
	Name           string             `bson:"name" json:"name"`
	Type           string             `bson:"type" json:"type"` // sedan | suv | van | 4x4
	Seats          int                `bson:"seats" json:"seats"`
	Fuel           string             `bson:"fuel" json:"fuel"` // petrol | diesel | hybrid | electric
	PricePerDayUSD float64            `bson:"price_per_day_usd" json:"price_per_day_usd"`
	Tags           []string           `bson:"tags" json:"tags"`
	CoverImage     Image              `bson:"cover_image" json:"cover_image"`
	Images         []Image            `bson:"images" json:"images"`
	IsActive       bool               `bson:"is_active" json:"is_active"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
	// UserID is the tenant_users._id of whoever last created/updated this
	// record via the admin panel. Nil if never touched by an authenticated
	// tenant user.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
}
