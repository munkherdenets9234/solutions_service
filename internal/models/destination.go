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

type ItineraryDay struct {
	Day         int      `bson:"day" json:"day"`
	Title       string   `bson:"title" json:"title"`
	Description string   `bson:"description" json:"description"`
	Activities  []string `bson:"activities" json:"activities"`
	Overnight   string   `bson:"overnight" json:"overnight"` // e.g. "Ger Camp", "Hotel"
	Meals       []string `bson:"meals" json:"meals"`         // e.g. ["breakfast","lunch","dinner"]
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
	Overview string             `bson:"overview" json:"overview"`
	Region   string             `bson:"region" json:"region"`
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

	// Content
	Highlights []string       `bson:"highlights" json:"highlights"`
	Activities []string       `bson:"activities" json:"activities"`
	Inclusions []string       `bson:"inclusions" json:"inclusions"`
	Exclusions []string       `bson:"exclusions" json:"exclusions"`
	Itinerary  []ItineraryDay `bson:"itinerary" json:"itinerary"`

	// Logistics
	Accommodation string `bson:"accommodation" json:"accommodation"` // e.g. "4-star hotel + ger camps"
	MealPlan      string `bson:"meal_plan" json:"meal_plan"`         // e.g. "all meals included"
	Difficulty    string `bson:"difficulty" json:"difficulty"`       // easy | moderate | challenging

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
