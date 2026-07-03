package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type BlogStatus string

const (
	BlogDraft     BlogStatus = "draft"
	BlogPublished BlogStatus = "published"
)

type Blog struct {
	ID            primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	TenantID      primitive.ObjectID  `bson:"tenant_id" json:"tenant_id"`
	Title         string              `bson:"title" json:"title"`
	Slug          string              `bson:"slug" json:"slug"`
	Excerpt       string              `bson:"excerpt" json:"excerpt"`
	Content       string              `bson:"content" json:"content"` // HTML or Markdown
	Author        string              `bson:"author" json:"author"`
	DestinationID *primitive.ObjectID `bson:"destination_id,omitempty" json:"destination_id,omitempty"`
	CoverImage    Image               `bson:"cover_image" json:"cover_image"`
	Images        []Image             `bson:"images" json:"images"`
	Tags          []string            `bson:"tags" json:"tags"`
	Status        BlogStatus          `bson:"status" json:"status"`
	Views         int64               `bson:"views" json:"views"`
	PublishedAt   *time.Time          `bson:"published_at,omitempty" json:"published_at,omitempty"`
	CreatedAt     time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time           `bson:"updated_at" json:"updated_at"`
}
