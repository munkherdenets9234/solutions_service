package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ContactStatus string

const (
	ContactNew       ContactStatus = "new"
	ContactRead      ContactStatus = "read"
	ContactResponded ContactStatus = "responded"
)

type ContactMessage struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID  primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Name      string             `bson:"name" json:"name"`
	Email     string             `bson:"email" json:"email"`
	Subject   string             `bson:"subject" json:"subject"`
	Message   string             `bson:"message" json:"message"`
	Status    ContactStatus      `bson:"status" json:"status"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
}
