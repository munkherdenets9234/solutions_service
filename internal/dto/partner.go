package dto

import (
	"time"

	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PartnerResponse is the public, single-locale shape of models.Partner.
type PartnerResponse struct {
	ID            primitive.ObjectID      `json:"id"`
	Name          string                  `json:"name"`
	Slug          string                  `json:"slug"`
	Tag           string                  `json:"tag"`
	Title         string                  `json:"title"`
	Description   string                  `json:"description"`
	Image         string                  `json:"image"`
	WebURL        string                  `json:"web_url"`
	Products      []models.PartnerProduct `json:"products"`
	RelatedReview *primitive.ObjectID     `json:"related_review,omitempty"`
	IsActive      bool                    `json:"is_active"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
}

func ToPartnerResponse(p *models.Partner, locale string) PartnerResponse {
	return PartnerResponse{
		ID:            p.ID,
		Name:          p.Name,
		Slug:          p.Slug,
		Tag:           p.Tag,
		Title:         i18n.Resolve(p.Title, locale),
		Description:   i18n.Resolve(p.Description, locale),
		Image:         p.Image,
		WebURL:        p.WebURL,
		Products:      p.Products,
		RelatedReview: p.RelatedReview,
		IsActive:      p.IsActive,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

func ToPartnerResponses(partners []*models.Partner, locale string) []PartnerResponse {
	out := make([]PartnerResponse, len(partners))
	for i, p := range partners {
		out[i] = ToPartnerResponse(p, locale)
	}
	return out
}
