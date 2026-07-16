package dto

import (
	"time"

	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type DestinationItineraryDayResponse struct {
	Day         int      `json:"day"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Activities  []string `json:"activities"`
	Overnight   string   `json:"overnight"`
	Meals       []string `json:"meals"`
}

func toItineraryDayResponse(d models.ItineraryDay, locale string) DestinationItineraryDayResponse {
	return DestinationItineraryDayResponse{
		Day:         d.Day,
		Title:       i18n.Resolve(d.Title, locale),
		Description: i18n.Resolve(d.Description, locale),
		Activities:  i18n.ResolveList(d.Activities, locale),
		Overnight:   i18n.Resolve(d.Overnight, locale),
		Meals:       i18n.ResolveList(d.Meals, locale),
	}
}

// DestinationResponse is the public, single-locale shape of models.Destination.
type DestinationResponse struct {
	ID       primitive.ObjectID `json:"id"`
	Name     string             `json:"name"`
	Slug     string             `json:"slug"`
	Overview string             `json:"overview"`
	Region   string             `json:"region"`
	Location struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"location"`

	DurationDays int `json:"duration_days"`
	GroupSize    struct {
		Min int `json:"min"`
		Max int `json:"max"`
	} `json:"group_size"`
	Prices      []models.PriceByGroup `json:"prices"`
	BestSeasons []string              `json:"best_seasons"`
	Departures  []models.Departure    `json:"departures"`

	Highlights []string                          `json:"highlights"`
	Activities []string                          `json:"activities"`
	Inclusions []string                          `json:"inclusions"`
	Exclusions []string                          `json:"exclusions"`
	Itinerary  []DestinationItineraryDayResponse `json:"itinerary"`

	Accommodation string `json:"accommodation"`
	MealPlan      string `json:"meal_plan"`
	Difficulty    string `json:"difficulty"`

	Categories []string `json:"categories"`
	Tags       []string `json:"tags"`

	CoverImage models.Image   `json:"cover_image"`
	Images     []models.Image `json:"images"`

	Featured  bool      `json:"featured"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func ToDestinationResponse(d *models.Destination, locale string) DestinationResponse {
	itinerary := make([]DestinationItineraryDayResponse, len(d.Itinerary))
	for i, day := range d.Itinerary {
		itinerary[i] = toItineraryDayResponse(day, locale)
	}

	out := DestinationResponse{
		ID:            d.ID,
		Name:          d.Name,
		Slug:          d.Slug,
		Overview:      i18n.Resolve(d.Overview, locale),
		Region:        d.Region,
		DurationDays:  d.DurationDays,
		Prices:        d.Prices,
		BestSeasons:   d.BestSeasons,
		Departures:    d.Departures,
		Highlights:    i18n.ResolveList(d.Highlights, locale),
		Activities:    i18n.ResolveList(d.Activities, locale),
		Inclusions:    i18n.ResolveList(d.Inclusions, locale),
		Exclusions:    i18n.ResolveList(d.Exclusions, locale),
		Itinerary:     itinerary,
		Accommodation: i18n.Resolve(d.Accommodation, locale),
		MealPlan:      i18n.Resolve(d.MealPlan, locale),
		Difficulty:    i18n.Resolve(d.Difficulty, locale),
		Categories:    d.Categories,
		Tags:          d.Tags,
		CoverImage:    d.CoverImage,
		Images:        d.Images,
		Featured:      d.Featured,
		IsActive:      d.IsActive,
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
	}
	out.Location.Lat = d.Location.Lat
	out.Location.Lng = d.Location.Lng
	out.GroupSize.Min = d.GroupSize.Min
	out.GroupSize.Max = d.GroupSize.Max
	return out
}

func ToDestinationResponses(destinations []*models.Destination, locale string) []DestinationResponse {
	out := make([]DestinationResponse, len(destinations))
	for i, d := range destinations {
		out[i] = ToDestinationResponse(d, locale)
	}
	return out
}
