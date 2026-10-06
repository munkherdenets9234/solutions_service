package service

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/apierr"
)

const (
	guideTextMax       = 2000
	guideMaxReferences = 5
	guideMaxFiles      = 8
	guideMinAge        = 18
)

// GuideUploadMeta describes one file for rule checks. Before upload Mime is ""
// and Size is 0; those checks run only once real values are present.
type GuideUploadMeta struct {
	Kind models.GuideFileKind
	Mime string
	Size int64
}

var guideAllowedMimes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"application/pdf": true,
}

var (
	guideGenders     = []string{"male", "female", "other", "undisclosed"}
	guideLocales     = []string{"mn", "en"}
	guideLevelsMn    = []string{"native", "good", "intermediate"}
	guideLevelsOther = []string{"native", "fluent", "intermediate", "basic"}
)

// NormalizeEmail trims and lowercases an email address.
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func guideFail(field, reason string) error {
	return apierr.ValidationFailed(field + ": " + reason)
}

func guideIn(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func guideText(field, v string) error {
	if utf8.RuneCountInString(v) > guideTextMax {
		return guideFail(field, "too long")
	}
	return nil
}

func guideEnumList(field string, vals, allowed []string) error {
	for _, v := range vals {
		if !guideIn(allowed, v) {
			return guideFail(field, "unknown value")
		}
	}
	return nil
}

func guidePhoneOK(s string) bool {
	s = strings.TrimPrefix(strings.TrimSpace(s), "+")
	n := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			n++
		case r == ' ' || r == '-' || r == '(' || r == ')':
		default:
			return false
		}
	}
	return n >= 7 && n <= 15
}

// ValidateGuideApplication checks the application and returns the first
// failure as a VALIDATION_FAILED error naming the field.
func ValidateGuideApplication(a *models.GuideApplication, now time.Time) error {
	p := &a.Personal
	if strings.TrimSpace(p.FullName) == "" {
		return guideFail("full_name", "required")
	}
	if strings.TrimSpace(p.Phone) == "" {
		return guideFail("phone", "required")
	}
	if !guidePhoneOK(p.Phone) {
		return guideFail("phone", "must have 7 to 15 digits")
	}
	email := NormalizeEmail(p.Email)
	if email == "" {
		return guideFail("email", "required")
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || !strings.Contains(email, "@") {
		return guideFail("email", "invalid format")
	}
	if p.BirthDate.IsZero() {
		return guideFail("birth_date", "required")
	}
	by, bm, bd := p.BirthDate.UTC().Date()
	ny, nm, nd := now.UTC().Date()
	age := ny - by
	if nm < bm || (nm == bm && nd < bd) {
		age--
	}
	if age < guideMinAge {
		return guideFail("birth_date", "applicant must be at least 18")
	}
	if p.Gender != "" && !guideIn(guideGenders, p.Gender) {
		return guideFail("gender", "unknown value")
	}
	if a.Locale != "" && !guideIn(guideLocales, a.Locale) {
		return guideFail("locale", "unknown value")
	}

	var hasMn, hasEn bool
	for _, l := range a.Languages {
		if !guideIn(models.GuideLanguageCodes, l.Language) {
			return guideFail("languages.language", "unknown value")
		}
		levels := guideLevelsOther
		if l.Language == "mn" {
			levels = guideLevelsMn
		}
		if !guideIn(levels, l.Level) {
			return guideFail("languages.level", "unknown value")
		}
		hasMn = hasMn || l.Language == "mn"
		hasEn = hasEn || l.Language == "en"
		if l.Language != "other" && l.OtherName != "" {
			return guideFail("languages.other_name", "only allowed for other")
		}
		if l.Language == "other" && strings.TrimSpace(l.OtherName) == "" {
			return guideFail("languages.other_name", "required for other")
		}
		if err := guideText("languages.other_name", l.OtherName); err != nil {
			return err
		}
	}
	if !hasMn {
		return guideFail("languages.mn", "required")
	}
	if !hasEn {
		return guideFail("languages.en", "required")
	}

	e := &a.Experience
	if e.Years < 0 {
		return guideFail("experience.years", "must not be negative")
	}
	if e.LargestGroup < 0 {
		return guideFail("experience.largest_group", "must not be negative")
	}
	if err := guideEnumList("experience.tour_types", e.TourTypes, models.GuideTourTypes); err != nil {
		return err
	}

	if len(a.Regions) == 0 {
		return guideFail("regions", "required")
	}
	if err := guideEnumList("regions", a.Regions, models.GuideRegions); err != nil {
		return err
	}

	d := &a.Driving
	if !d.HasLicense && (d.LicenseClass != "" || d.YearsDriving != 0 || d.CanDrive4x4 ||
		d.LongDistance || d.HasOwnVehicle || d.Vehicles != "") {
		return guideFail("driving", "details only accepted when has_license is true")
	}
	if d.YearsDriving < 0 {
		return guideFail("driving.years_driving", "must not be negative")
	}

	av := &a.Availability
	if len(av.Months) == 0 {
		return guideFail("availability.months", "required")
	}
	for _, m := range av.Months {
		if m < 1 || m > 12 {
			return guideFail("availability.months", "must be 1 to 12")
		}
	}
	if err := guideEnumList("availability.trip_lengths", av.TripLengths, models.GuideTripLengths); err != nil {
		return err
	}

	if len(a.References) > guideMaxReferences {
		return guideFail("references", "at most 5")
	}
	for _, r := range a.References {
		if err := guideText("references.name", r.Name); err != nil {
			return err
		}
		if err := guideText("references.position", r.Position); err != nil {
			return err
		}
		if err := guideText("references.contact", r.Contact); err != nil {
			return err
		}
	}

	texts := []struct{ f, v string }{
		{"full_name", p.FullName}, {"nickname", p.Nickname}, {"address", p.Address},
		{"emergency_contact.name", p.EmergencyContact.Name}, {"emergency_contact.phone", p.EmergencyContact.Phone},
		{"experience.previous_companies", e.PreviousCompanies}, {"experience.main_directions", e.MainDirections},
		{"regions_other", a.RegionsOther}, {"driving.license_class", d.LicenseClass}, {"driving.vehicles", d.Vehicles},
		{"availability.days", av.Days}, {"availability.booked_trips", av.BookedTrips},
	}
	for _, t := range texts {
		if err := guideText(t.f, t.v); err != nil {
			return err
		}
	}

	if a.ConsentAt.IsZero() {
		return guideFail("consent_at", "required")
	}
	return nil
}

// ValidateGuideFiles checks count, per-kind caps, kind enum and the required
// cv. Mime and size are checked only when set (the post-upload call).
func ValidateGuideFiles(files []GuideUploadMeta) error {
	if len(files) > guideMaxFiles {
		return guideFail("files", fmt.Sprintf("at most %d", guideMaxFiles))
	}
	counts := map[models.GuideFileKind]int{}
	for _, f := range files {
		known := false
		for _, k := range models.GuideFileKinds {
			if k == f.Kind {
				known = true
				break
			}
		}
		if !known {
			return guideFail("files.kind", "unknown value")
		}
		counts[f.Kind]++
		limit := 1
		if f.Kind == models.GuideFileGuideCertificate {
			limit = 3
		}
		if counts[f.Kind] > limit {
			return guideFail("files."+string(f.Kind), fmt.Sprintf("at most %d", limit))
		}
		if f.Mime != "" && !guideAllowedMimes[f.Mime] {
			return guideFail("files."+string(f.Kind), "unsupported file type")
		}
		if f.Mime != "" && f.Size <= 0 {
			return guideFail("files."+string(f.Kind), "empty file")
		}
	}
	if counts[models.GuideFileCV] == 0 {
		return guideFail("files.cv", "required")
	}
	return nil
}
