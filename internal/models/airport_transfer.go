package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type TransferTier string
type TransferStatus string

const (
	TransferStandard TransferTier = "standard"
	TransferPremium  TransferTier = "premium"
	TransferVIP      TransferTier = "vip"

	TransferPending   TransferStatus = "pending"
	TransferConfirmed TransferStatus = "confirmed"
	TransferCancelled TransferStatus = "cancelled"
	TransferCompleted TransferStatus = "completed"
)

type AirportTransfer struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID       primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	CustomerID     primitive.ObjectID `bson:"customer_id" json:"customer_id"`
	Tier           TransferTier       `bson:"tier" json:"tier"`
	FlightNumber   string             `bson:"flight_number" json:"flight_number"`
	ArrivalAt      time.Time          `bson:"arrival_at" json:"arrival_at"`
	Passengers     int                `bson:"passengers" json:"passengers"`
	Notes          string             `bson:"notes" json:"notes"`
	Status         TransferStatus     `bson:"status" json:"status"`
	ConfirmationID string             `bson:"confirmation_id" json:"confirmation_id"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
	// UserID is the tenant_users._id of the admin who last changed this
	// transfer's status. Nil until an admin acts on it — transfers are
	// created by public, unauthenticated customer submissions.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read — not persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
}
