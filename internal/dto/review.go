package dto

import (
	"time"

	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ReviewResponse is the public, single-locale shape of models.Review.
type ReviewResponse struct {
	ID              primitive.ObjectID `json:"id"`
	RelatedCustomer string             `json:"related_customer"`
	CustomerAvatar  string             `json:"customer_avatar,omitempty"`
	Star            int                `json:"star"`
	Review          string             `json:"review"`
	RelatedTour     string             `json:"related_tour"`
	RelatedPartner  string             `json:"related_partner"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type CreatePublicReviewRequest struct {
	Name   string `json:"name" binding:"required"`
	Star   int    `json:"star" binding:"required"`
	Review string `json:"review" binding:"required"`
	Tour   string `json:"related_tour"`
}

func (req CreatePublicReviewRequest) ToModel(locale string) *models.Review {
	return &models.Review{
		RelatedCustomer: req.Name,
		Star:            req.Star,
		Review:          map[string]string{locale: req.Review},
		RelatedTour:     req.Tour,
	}
}

func ToReviewResponse(r *models.Review, locale string) ReviewResponse {
	return ReviewResponse{
		ID:              r.ID,
		RelatedCustomer: r.RelatedCustomer,
		Star:            r.Star,
		Review:          i18n.Resolve(r.Review, locale),
		RelatedTour:     r.RelatedTour,
		RelatedPartner:  r.RelatedPartner,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

// ToReviewResponsesWithAvatars is ToReviewResponses plus the avatar of each
// review's linked customer, looked up by customer id in one batch beforehand.
// Only the avatar URL is exposed: nothing else about the customer is public.
func ToReviewResponsesWithAvatars(reviews []*models.Review, locale string, avatars map[primitive.ObjectID]string) []ReviewResponse {
	out := ToReviewResponses(reviews, locale)
	for i, r := range reviews {
		if r.CustomerID != nil {
			out[i].CustomerAvatar = avatars[*r.CustomerID]
		}
	}
	return out
}

func ToReviewResponses(reviews []*models.Review, locale string) []ReviewResponse {
	out := make([]ReviewResponse, len(reviews))
	for i, r := range reviews {
		out[i] = ToReviewResponse(r, locale)
	}
	return out
}
