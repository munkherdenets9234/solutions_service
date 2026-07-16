package dto

import (
	"time"

	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// BlogResponse is the public, single-locale shape of models.Blog — the
// locale maps on the model are resolved down to flat strings for the
// requested locale (see internal/i18n).
type BlogResponse struct {
	ID            primitive.ObjectID  `json:"id"`
	Title         string              `json:"title"`
	Slug          string              `json:"slug"`
	Category      string              `json:"category"`
	Excerpt       string              `json:"excerpt"`
	Content       string              `json:"content"`
	Quote         string              `json:"quote"`
	Author        models.BlogAuthor   `json:"author"`
	ReadTime      int                 `json:"read_time"`
	Date          string              `json:"date"`
	Featured      bool                `json:"featured"`
	Image         string              `json:"image"`
	DestinationID *primitive.ObjectID `json:"destination_id,omitempty"`
	CoverImage    models.Image        `json:"cover_image"`
	Images        []models.Image      `json:"images"`
	Tags          []string            `json:"tags"`
	Status        models.BlogStatus   `json:"status"`
	Views         int64               `json:"views"`
	PublishedAt   *time.Time          `json:"published_at,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

func ToBlogResponse(b *models.Blog, locale string) BlogResponse {
	return BlogResponse{
		ID:            b.ID,
		Title:         i18n.Resolve(b.Title, locale),
		Slug:          b.Slug,
		Category:      b.Category,
		Excerpt:       i18n.Resolve(b.Excerpt, locale),
		Content:       i18n.Resolve(b.Content, locale),
		Quote:         i18n.Resolve(b.Quote, locale),
		Author:        b.Author,
		ReadTime:      b.ReadTime,
		Date:          b.Date,
		Featured:      b.Featured,
		Image:         b.Image,
		DestinationID: b.DestinationID,
		CoverImage:    b.CoverImage,
		Images:        b.Images,
		Tags:          b.Tags,
		Status:        b.Status,
		Views:         b.Views,
		PublishedAt:   b.PublishedAt,
		CreatedAt:     b.CreatedAt,
		UpdatedAt:     b.UpdatedAt,
	}
}

func ToBlogResponses(blogs []*models.Blog, locale string) []BlogResponse {
	out := make([]BlogResponse, len(blogs))
	for i, b := range blogs {
		out[i] = ToBlogResponse(b, locale)
	}
	return out
}
