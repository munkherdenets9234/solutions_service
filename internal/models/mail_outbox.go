package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type MailStatus string

const (
	MailPending MailStatus = "pending"
	MailSent    MailStatus = "sent"
	MailFailed  MailStatus = "failed"
)

// MailOutbox is one queued staff notification. The address is copied onto the
// row at enqueue time so a later change of a user's email cannot redirect a
// queued message. LastError is a short code only (for example "rate_limited"),
// never provider text: a provider's error body can echo the message or
// credentials.
type MailOutbox struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	UserID   primitive.ObjectID `bson:"user_id" json:"user_id"`
	To       string             `bson:"to" json:"to"`
	Kind     string             `bson:"kind" json:"kind"`
	RecordID primitive.ObjectID `bson:"record_id" json:"record_id"`
	Data     map[string]string  `bson:"data" json:"data"`

	Status        MailStatus `bson:"status" json:"status"`
	Attempts      int        `bson:"attempts" json:"attempts"`
	NextAttemptAt time.Time  `bson:"next_attempt_at" json:"next_attempt_at"`
	LastError     string     `bson:"last_error,omitempty" json:"last_error,omitempty"`

	CreatedAt time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time  `bson:"updated_at" json:"updated_at"`
	SentAt    *time.Time `bson:"sent_at,omitempty" json:"sent_at,omitempty"`
}
