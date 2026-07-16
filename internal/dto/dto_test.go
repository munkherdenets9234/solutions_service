package dto

import (
	"testing"

	"github.com/eandstravel/digitalservice/internal/models"
)

func TestToBlogResponse(t *testing.T) {
	b := &models.Blog{
		Title:   map[string]string{"en": "Hello", "mn": "Сайн байна уу"},
		Excerpt: map[string]string{"en": "An excerpt"},
		Content: map[string]string{"en": "<p>Content</p>"},
		Slug:    "hello",
	}

	en := ToBlogResponse(b, "en")
	if en.Title != "Hello" || en.Excerpt != "An excerpt" || en.Content != "<p>Content</p>" {
		t.Errorf("en response = %+v", en)
	}
	if en.Slug != "hello" {
		t.Errorf("Slug not preserved: %q", en.Slug)
	}

	mn := ToBlogResponse(b, "mn")
	if mn.Title != "Сайн байна уу" {
		t.Errorf("mn Title = %q, want mn value", mn.Title)
	}
	// Excerpt has no mn translation — must fall back to en, not go blank.
	if mn.Excerpt != "An excerpt" {
		t.Errorf("mn Excerpt = %q, want fallback to en", mn.Excerpt)
	}
}

func TestToPartnerResponse_NameNotTranslated(t *testing.T) {
	p := &models.Partner{
		Name:        "Nomad Camp LLC",
		Title:       map[string]string{"en": "Ger camps", "mn": "Гэр буудал"},
		Description: map[string]string{"en": "Family-run"},
	}

	mn := ToPartnerResponse(p, "mn")
	if mn.Name != "Nomad Camp LLC" {
		t.Errorf("Name should stay untranslated, got %q", mn.Name)
	}
	if mn.Title != "Гэр буудал" {
		t.Errorf("Title = %q, want mn value", mn.Title)
	}
	if mn.Description != "Family-run" {
		t.Errorf("Description should fall back to en, got %q", mn.Description)
	}
}

func TestToDestinationResponse_ItineraryAndLists(t *testing.T) {
	d := &models.Destination{
		Name:       "Gobi Desert Classic",
		Overview:   map[string]string{"en": "A journey", "mn": "Аялал"},
		Highlights: map[string][]string{"en": {"Dunes", "Camels"}, "mn": {"Манхан", "Тэмээ"}},
		Itinerary: []models.ItineraryDay{
			{
				Day:         1,
				Title:       map[string]string{"en": "Arrival", "mn": "Ирэлт"},
				Description: map[string]string{"en": "Fly in"},
				Activities:  map[string][]string{"en": {"Hiking"}},
				Overnight:   map[string]string{"en": "Ger Camp", "mn": "Гэр буудал"},
				Meals:       map[string][]string{"en": {"dinner"}},
			},
		},
	}

	mn := ToDestinationResponse(d, "mn")
	if mn.Name != "Gobi Desert Classic" {
		t.Errorf("Name should stay untranslated, got %q", mn.Name)
	}
	if mn.Overview != "Аялал" {
		t.Errorf("Overview = %q, want mn value", mn.Overview)
	}
	if len(mn.Highlights) != 2 || mn.Highlights[0] != "Манхан" {
		t.Errorf("Highlights = %v, want mn list", mn.Highlights)
	}
	if len(mn.Itinerary) != 1 {
		t.Fatalf("Itinerary length = %d, want 1", len(mn.Itinerary))
	}
	day := mn.Itinerary[0]
	if day.Day != 1 {
		t.Errorf("Day = %d, want 1", day.Day)
	}
	if day.Title != "Ирэлт" {
		t.Errorf("Itinerary Title = %q, want mn value", day.Title)
	}
	if day.Description != "Fly in" {
		t.Errorf("Itinerary Description should fall back to en, got %q", day.Description)
	}
	if day.Overnight != "Гэр буудал" {
		t.Errorf("Itinerary Overnight = %q, want mn value", day.Overnight)
	}
	if len(day.Meals) != 1 || day.Meals[0] != "dinner" {
		t.Errorf("Itinerary Meals should fall back to en, got %v", day.Meals)
	}
}

func TestToDestinationResponses_Empty(t *testing.T) {
	if got := ToDestinationResponses(nil, "en"); len(got) != 0 {
		t.Errorf("expected empty slice for nil input, got %v", got)
	}
}

func TestToReviewResponse(t *testing.T) {
	r := &models.Review{
		Star:            5,
		Review:          map[string]string{"en": "Amazing trip", "mn": "Гайхалтай аялал"},
		RelatedCustomer: "Anna K.",
	}

	mn := ToReviewResponse(r, "mn")
	if mn.Review != "Гайхалтай аялал" {
		t.Errorf("Review = %q, want mn value", mn.Review)
	}
	if mn.RelatedCustomer != "Anna K." {
		t.Errorf("RelatedCustomer should stay untranslated, got %q", mn.RelatedCustomer)
	}

	fr := ToReviewResponse(r, "fr")
	if fr.Review != "Amazing trip" {
		t.Errorf("unsupported locale should fall back to en, got %q", fr.Review)
	}
}
