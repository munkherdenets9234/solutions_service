package dto

import (
	"time"

	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PackageResponse is the public, single-locale shape of models.Package.
type PackageResponse struct {
	ID          primitive.ObjectID `json:"id"`
	Slug        string             `json:"slug"`
	Name        string             `json:"name"`
	Tagline     string             `json:"tagline"`
	Price       float64            `json:"price"`
	Currency    string             `json:"currency"`
	BillingNote string             `json:"billing_note"`
	Features    []string           `json:"features"`
	Highlighted bool               `json:"highlighted"`
	SortOrder   int                `json:"sort_order"`
	IsActive    bool               `json:"is_active"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

func ToPackageResponse(p *models.Package, locale string) PackageResponse {
	return PackageResponse{
		ID:          p.ID,
		Slug:        p.Slug,
		Name:        i18n.Resolve(p.Name, locale),
		Tagline:     i18n.Resolve(p.Tagline, locale),
		Price:       p.Price,
		Currency:    p.Currency,
		BillingNote: i18n.Resolve(p.BillingNote, locale),
		Features:    i18n.ResolveList(p.Features, locale),
		Highlighted: p.Highlighted,
		SortOrder:   p.SortOrder,
		IsActive:    p.IsActive,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func ToPackageResponses(pkgs []*models.Package, locale string) []PackageResponse {
	out := make([]PackageResponse, len(pkgs))
	for i, p := range pkgs {
		out[i] = ToPackageResponse(p, locale)
	}
	return out
}
