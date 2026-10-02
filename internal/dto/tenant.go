package dto

import (
	"time"

	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ProjectMetricResponse is the public, single-locale shape of models.ProjectMetric.
type ProjectMetricResponse struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ProjectResponse is the public, single-locale "Our Projects" card/detail
// shape, joining models.Tenant (identity) with models.TenantDetail
// (case-study content) — deliberately excludes administrative tenant fields
// (contact_email, status, api_key_last4); GET /platform/tenants still
// returns those in full, plus the raw TenantDetail embedded as `project`,
// for the platform's own tenant management.
type ProjectResponse struct {
	ID          primitive.ObjectID `json:"id"`
	Name        string             `json:"name"`
	Slug        string             `json:"slug"`
	Tagline     string             `json:"tagline"`
	Description string             `json:"description"`
	Category    string             `json:"category"`
	// LiveURL is TenantDetail.WebsiteURL if set, otherwise falls back to
	// the tenant's own bound Domain (see PUT /platform/tenants/{id}/domain) —
	// so a project still shows a "visit website" link even before an admin
	// fills in the explicit field.
	LiveURL    string       `json:"live_url,omitempty"`
	CoverImage models.Image `json:"cover_image"`
	// AdminCover is a separate, admin-curated image for front-page/list
	// display — see models.TenantDetail.AdminCover.
	AdminCover models.Image            `json:"admin_cover"`
	Images     []models.Image          `json:"images"`
	Metrics    []ProjectMetricResponse `json:"metrics"`
	Featured   bool                    `json:"featured"`
	SortOrder  int                     `json:"sort_order"`
	CreatedAt  time.Time               `json:"created_at"`
	UpdatedAt  time.Time               `json:"updated_at"`
}

func ToProjectResponse(t *models.Tenant, d *models.TenantDetail, locale string) ProjectResponse {
	metrics := make([]ProjectMetricResponse, len(d.Metrics))
	for i, m := range d.Metrics {
		metrics[i] = ProjectMetricResponse{Label: i18n.Resolve(m.Label, locale), Value: m.Value}
	}

	liveURL := d.WebsiteURL
	if liveURL == "" && t.Domain != "" {
		liveURL = "https://" + t.Domain
	}

	return ProjectResponse{
		ID:          t.ID,
		Name:        t.Name,
		Slug:        t.Slug,
		Tagline:     i18n.Resolve(d.Tagline, locale),
		Description: i18n.Resolve(d.Description, locale),
		Category:    d.Category,
		LiveURL:     liveURL,
		CoverImage:  d.CoverImage,
		AdminCover:  d.AdminCover,
		Images:      d.Images,
		Metrics:     metrics,
		Featured:    d.Featured,
		SortOrder:   d.SortOrder,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

// ToProjectResponses converts parallel tenants/details slices (tenants[i]
// paired with details[i], as returned by TenantService.ListProjects).
func ToProjectResponses(tenants []*models.Tenant, details []*models.TenantDetail, locale string) []ProjectResponse {
	out := make([]ProjectResponse, len(tenants))
	for i := range tenants {
		out[i] = ToProjectResponse(tenants[i], details[i], locale)
	}
	return out
}
