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

// BlogAuthor is the byline shown on an article - a name plus a short role
// like "Lead guide". Name as stored here is just editorial text, not a
// TenantUser reference — but BlogService.resolveAuthors overwrites it on
// every read with the display name of whoever last created/updated the
// blog (Blog.UserID), when that's known, so GET responses always show the
// current editor rather than a possibly stale typed-in byline.
type BlogAuthor struct {
	Name string `bson:"name" json:"name"`
	Role string `bson:"role" json:"role"`
}

// BlogSection is one heading+text block of a blog's structured body, as
// opposed to Content, which holds a single HTML/Markdown blob.
type BlogSection struct {
	Heading string `bson:"heading" json:"heading"`
	Text    string `bson:"text" json:"text"`
}

type Blog struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	// Title, Excerpt, Content, Quote are locale maps (e.g. {"en": "...", "mn": "..."})
	// — see internal/i18n for resolving them to a single locale.
	Title         map[string]string   `bson:"title" json:"title"`
	Slug          string              `bson:"slug" json:"slug"`
	Category      string              `bson:"category" json:"category"`
	Excerpt       map[string]string   `bson:"excerpt" json:"excerpt"`
	Content       map[string]string   `bson:"content" json:"content"` // HTML or Markdown, per locale
	Body          []BlogSection       `bson:"body" json:"body"`       // structured alternative to Content — not localized
	Quote         map[string]string   `bson:"quote" json:"quote"`
	Author        BlogAuthor          `bson:"author" json:"author"`
	ReadTime      int                 `bson:"read_time" json:"read_time"` // estimated minutes to read
	Date          string              `bson:"date" json:"date"`           // editorial display date, e.g. "2026-06-12" - independent of published_at
	Featured      bool                `bson:"featured" json:"featured"`
	Image         string              `bson:"image" json:"image"` // simple hero image URL, distinct from CoverImage/Images below
	DestinationID *primitive.ObjectID `bson:"destination_id,omitempty" json:"destination_id,omitempty"`
	CoverImage    Image               `bson:"cover_image" json:"cover_image"`
	Images        []Image             `bson:"images" json:"images"`
	Tags          []string            `bson:"tags" json:"tags"`
	Status        BlogStatus          `bson:"status" json:"status"`
	Views         int64               `bson:"views" json:"views"`
	PublishedAt   *time.Time          `bson:"published_at,omitempty" json:"published_at,omitempty"`
	CreatedAt     time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time           `bson:"updated_at" json:"updated_at"`
	// UserID is the tenant_users._id of whoever last created/updated this
	// record via the admin panel. Nil if never touched by an authenticated
	// tenant user.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read (see BlogService.resolveLastEditedBy) — not
	// persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
}
