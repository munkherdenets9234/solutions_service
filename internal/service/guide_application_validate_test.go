package service

import (
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
)

var guideNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func validGuideApp() *models.GuideApplication {
	return &models.GuideApplication{
		Locale: "en",
		Personal: models.GuidePersonal{
			FullName: "Test Guide", Phone: "+976 9400 6739", Email: "guide@example.com",
			BirthDate: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), Gender: "male",
		},
		Languages:    []models.GuideLanguage{{Language: "mn", Level: "native"}, {Language: "en", Level: "fluent"}},
		Regions:      []string{"gobi"},
		Availability: models.GuideAvailability{Months: []int{6, 7}},
		ConsentAt:    guideNow,
	}
}

func expectGuideErr(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error naming %q, got nil", field)
	}
	if !strings.Contains(err.Error(), field) {
		t.Fatalf("error %q does not name %q", err.Error(), field)
	}
}

func TestRequiredFields(t *testing.T) {
	if err := ValidateGuideApplication(validGuideApp(), guideNow); err != nil {
		t.Fatalf("valid app rejected: %v", err)
	}
	cases := []struct {
		field string
		mod   func(a *models.GuideApplication)
	}{
		{"full_name", func(a *models.GuideApplication) { a.Personal.FullName = " " }},
		{"phone", func(a *models.GuideApplication) { a.Personal.Phone = "" }},
		{"email", func(a *models.GuideApplication) { a.Personal.Email = "" }},
		{"email", func(a *models.GuideApplication) { a.Personal.Email = "not-an-email" }},
		{"phone", func(a *models.GuideApplication) { a.Personal.Phone = "123456" }},
		{"birth_date", func(a *models.GuideApplication) { a.Personal.BirthDate = time.Time{} }},
		{"languages.mn", func(a *models.GuideApplication) { a.Languages = a.Languages[1:] }},
		{"languages.en", func(a *models.GuideApplication) { a.Languages = a.Languages[:1] }},
		{"regions", func(a *models.GuideApplication) { a.Regions = nil }},
		{"availability.months", func(a *models.GuideApplication) { a.Availability.Months = nil }},
		{"consent_at", func(a *models.GuideApplication) { a.ConsentAt = time.Time{} }},
	}
	for _, c := range cases {
		a := validGuideApp()
		c.mod(a)
		expectGuideErr(t, ValidateGuideApplication(a, guideNow), c.field)
	}
}

func TestEnumsRejectUnknown(t *testing.T) {
	cases := []struct {
		field string
		mod   func(a *models.GuideApplication)
	}{
		{"languages.language", func(a *models.GuideApplication) {
			a.Languages = append(a.Languages, models.GuideLanguage{Language: "xx", Level: "native"})
		}},
		{"languages.level", func(a *models.GuideApplication) { a.Languages[0].Level = "fluent" }}, // fluent not valid for mn
		{"languages.level", func(a *models.GuideApplication) { a.Languages[1].Level = "good" }},   // good not valid for en
		{"regions", func(a *models.GuideApplication) { a.Regions = []string{"mars"} }},
		{"experience.tour_types", func(a *models.GuideApplication) { a.Experience.TourTypes = []string{"nope"} }},
		{"availability.trip_lengths", func(a *models.GuideApplication) { a.Availability.TripLengths = []string{"d99"} }},
		{"gender", func(a *models.GuideApplication) { a.Personal.Gender = "robot" }},
		{"availability.months", func(a *models.GuideApplication) { a.Availability.Months = []int{13} }},
	}
	for _, c := range cases {
		a := validGuideApp()
		c.mod(a)
		expectGuideErr(t, ValidateGuideApplication(a, guideNow), c.field)
	}
}

func TestMongolianNameAndPhoneAccepted(t *testing.T) {
	a := validGuideApp()
	a.Personal.FullName = "Бат-Эрдэнэ Мөнхбаяр"
	a.Personal.Phone = "+976 9400 6739"
	if err := ValidateGuideApplication(a, guideNow); err != nil {
		t.Fatalf("rejected: %v", err)
	}
}

func TestAgeBoundary(t *testing.T) {
	a := validGuideApp()
	a.Personal.BirthDate = guideNow.AddDate(-18, 0, 0)
	if err := ValidateGuideApplication(a, guideNow); err != nil {
		t.Fatalf("exactly 18 rejected: %v", err)
	}
	a.Personal.BirthDate = guideNow.AddDate(-18, 0, 1)
	expectGuideErr(t, ValidateGuideApplication(a, guideNow), "birth_date")
}

func TestDrivingFieldsOnlyWhenLicense(t *testing.T) {
	a := validGuideApp()
	a.Driving = models.GuideDriving{HasLicense: false, LicenseClass: "B"}
	expectGuideErr(t, ValidateGuideApplication(a, guideNow), "driving")
	a.Driving = models.GuideDriving{HasLicense: true, LicenseClass: "B", YearsDriving: 5}
	if err := ValidateGuideApplication(a, guideNow); err != nil {
		t.Fatalf("licensed driving rejected: %v", err)
	}
}

func TestReferenceCap(t *testing.T) {
	a := validGuideApp()
	a.References = make([]models.GuideReference, 5)
	if err := ValidateGuideApplication(a, guideNow); err != nil {
		t.Fatalf("5 refs rejected: %v", err)
	}
	a.References = make([]models.GuideReference, 6)
	expectGuideErr(t, ValidateGuideApplication(a, guideNow), "references")
}

func TestTextCap(t *testing.T) {
	a := validGuideApp()
	a.Experience.PreviousCompanies = strings.Repeat("я", 2000)
	if err := ValidateGuideApplication(a, guideNow); err != nil {
		t.Fatalf("2000 runes rejected: %v", err)
	}
	a.Experience.PreviousCompanies = strings.Repeat("я", 2001)
	expectGuideErr(t, ValidateGuideApplication(a, guideNow), "experience.previous_companies")
}

func TestConsentRequired(t *testing.T) {
	a := validGuideApp()
	a.ConsentAt = time.Time{}
	expectGuideErr(t, ValidateGuideApplication(a, guideNow), "consent_at")
}

func TestFilesRules(t *testing.T) {
	m := func(kinds ...models.GuideFileKind) []GuideUploadMeta {
		var out []GuideUploadMeta
		for _, k := range kinds {
			out = append(out, GuideUploadMeta{Kind: k})
		}
		return out
	}
	cv := models.GuideFileCV
	cert := models.GuideFileGuideCertificate
	cases := []struct {
		name  string
		files []GuideUploadMeta
		field string // "" = ok
	}{
		{"cv only", m(cv), ""},
		{"no cv", m(models.GuideFilePhoto), "files.cv"},
		{"nine files", m(cv, models.GuideFilePhoto, models.GuideFileIDCard, models.GuideFileDriverLicense, models.GuideFileFirstAid, cert, cert, cert, cert), "files"},
		{"two id_card", m(cv, models.GuideFileIDCard, models.GuideFileIDCard), "files.id_card"},
		{"three certs", m(cv, cert, cert, cert), ""},
		{"four certs", m(cv, cert, cert, cert, cert), "files.guide_certificate"},
		{"unknown kind", m(cv, "passport"), "files.kind"},
		{"real ok", []GuideUploadMeta{{Kind: cv, Mime: "application/pdf", Size: 10}}, ""},
		{"bad mime", []GuideUploadMeta{{Kind: cv, Mime: "text/html", Size: 10}}, "files.cv"},
		{"zero size with mime", []GuideUploadMeta{{Kind: cv, Mime: "application/pdf", Size: 0}}, "files.cv"},
	}
	for _, c := range cases {
		err := ValidateGuideFiles(c.files)
		if c.field == "" {
			if err != nil {
				t.Errorf("%s: unexpected %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: want error naming %q, got %v", c.name, c.field, err)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Guide@Example.COM "); got != "guide@example.com" {
		t.Fatalf("got %q", got)
	}
}
