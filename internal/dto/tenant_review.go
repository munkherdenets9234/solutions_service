package dto

import (
	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TenantReviewResponse is the public, single-locale shape of
// models.TenantReview — just the fields the Review page displays.
type TenantReviewResponse struct {
	ID               primitive.ObjectID `json:"id"`
	Rate             int                `json:"rate"`
	Comment          string             `json:"comment"`
	CompanyName      string             `json:"company_name"`
	CompanyOwnerName string             `json:"company_owner_name"`
}

func ToTenantReviewResponse(r *models.TenantReview, locale string) TenantReviewResponse {
	return TenantReviewResponse{
		ID:               r.ID,
		Rate:             r.Rate,
		Comment:          i18n.Resolve(r.Comment, locale),
		CompanyName:      r.CompanyName,
		CompanyOwnerName: r.CompanyOwnerName,
	}
}

func ToTenantReviewResponses(reviews []*models.TenantReview, locale string) []TenantReviewResponse {
	out := make([]TenantReviewResponse, len(reviews))
	for i, r := range reviews {
		out[i] = ToTenantReviewResponse(r, locale)
	}
	return out
}
