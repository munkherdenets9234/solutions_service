package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Image struct {
	URL     string `bson:"url" json:"url"`
	Caption string `bson:"caption" json:"caption"`
}

type PriceByGroup struct {
	MinPeople int     `bson:"min_people" json:"min_people"`
	MaxPeople int     `bson:"max_people" json:"max_people"`
	PriceUSD  float64 `bson:"price_usd" json:"price_usd"`
}

// ItineraryDay's Title, Description, Overnight, Activities, and Meals are
// locale maps — see internal/i18n.
type ItineraryDay struct {
	Day         int                 `bson:"day" json:"day"`
	Title       map[string]string   `bson:"title" json:"title"`
	Description map[string]string   `bson:"description" json:"description"`
	Activities  map[string][]string `bson:"activities" json:"activities"`
	Overnight   map[string]string   `bson:"overnight" json:"overnight"` // e.g. "Ger Camp", "Hotel"
	Meals       map[string][]string `bson:"meals" json:"meals"`         // e.g. ["breakfast","lunch","dinner"]
}

type Departure struct {
	StartDate time.Time `bson:"start_date" json:"start_date"`
	EndDate   time.Time `bson:"end_date" json:"end_date"`
	Available bool      `bson:"available" json:"available"`
}

type Destination struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Name     string             `bson:"name" json:"name"`
	Slug     string             `bson:"slug" json:"slug"`
	// Overview is a locale map — see internal/i18n.
	Overview map[string]string `bson:"overview" json:"overview"`
	Region   string            `bson:"region" json:"region"`
	Location struct {
		Lat float64 `bson:"lat" json:"lat"`
		Lng float64 `bson:"lng" json:"lng"`
	} `bson:"location" json:"location"`

	// Tour specs (from discovermongolia.mn)
	DurationDays int `bson:"duration_days" json:"duration_days"`
	GroupSize    struct {
		Min int `bson:"min" json:"min"`
		Max int `bson:"max" json:"max"`
	} `bson:"group_size" json:"group_size"`
	Prices      []PriceByGroup `bson:"prices" json:"prices"`
	BestSeasons []string       `bson:"best_seasons" json:"best_seasons"` // "spring","summer","autumn","winter"
	Departures  []Departure    `bson:"departures" json:"departures"`

	// Content — Highlights, Activities, Inclusions, Exclusions are locale maps
	// (see internal/i18n); Itinerary's own translatable fields are on ItineraryDay.
	Highlights map[string][]string `bson:"highlights" json:"highlights"`
	Activities map[string][]string `bson:"activities" json:"activities"`
	Inclusions map[string][]string `bson:"inclusions" json:"inclusions"`
	Exclusions map[string][]string `bson:"exclusions" json:"exclusions"`
	Itinerary  []ItineraryDay      `bson:"itinerary" json:"itinerary"`

	// Logistics — Accommodation, MealPlan, Difficulty are locale maps.
	Accommodation map[string]string `bson:"accommodation" json:"accommodation"` // e.g. "4-star hotel + ger camps"
	MealPlan      map[string]string `bson:"meal_plan" json:"meal_plan"`         // e.g. "all meals included"
	Difficulty    map[string]string `bson:"difficulty" json:"difficulty"`       // easy | moderate | challenging

	// Categories & tags
	Categories []string `bson:"categories" json:"categories"` // adventure, cultural, wildlife, scenic
	Tags       []string `bson:"tags" json:"tags"`

	// Media
	CoverImage Image   `bson:"cover_image" json:"cover_image"`
	Images     []Image `bson:"images" json:"images"`

	Featured  bool      `bson:"featured" json:"featured"`
	IsActive  bool      `bson:"is_active" json:"is_active"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}
