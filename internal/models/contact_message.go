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
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
	// UserID is the tenant_users._id of the admin who last changed this
	// message's status. Nil until an admin acts on it — contact messages are
	// created by public, unauthenticated visitors.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read — not persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
}
