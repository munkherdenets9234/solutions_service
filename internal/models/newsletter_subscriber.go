package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type NewsletterSubscriber struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID  primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Email     string             `bson:"email" json:"email"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
}
