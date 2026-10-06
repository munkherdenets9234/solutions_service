package service

import (
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/apierr"
)

const errInvalidCarFields = "invalid car fields"

// CarModes returns the rental modes a car offers; both when none are stored.
func CarModes(c *models.Car) []string {
	if len(c.RentalModes) == 0 {
		return []string{models.CarModeWithDriver, models.CarModeSelfDrive}
	}
	return c.RentalModes
}

// CarVisible reports whether the car is shown publicly. A car with no stored
// visibility (legacy) is visible.
func CarVisible(c *models.Car) bool {
	return c.IsVisible == nil || *c.IsVisible
}

func hasMode(modes []string, m string) bool {
	for _, x := range modes {
		if x == m {
			return true
		}
	}
	return false
}

func validModes(modes []string) bool {
	for _, m := range modes {
		if m != models.CarModeWithDriver && m != models.CarModeSelfDrive {
			return false
		}
	}
	return true
}

// dateOnly reduces t to its own calendar date at UTC midnight.
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// utcDateOnly reduces t to its UTC calendar date.
func utcDateOnly(t time.Time) time.Time { return dateOnly(t.UTC()) }

// validRange checks a self-drive range: both set or both nil, from <= to.
func validRange(from, to *time.Time) bool {
	if (from == nil) != (to == nil) {
		return false
	}
	return from == nil || !to.Before(*from)
}

// PrepareCarCreate applies defaults and validates a new car.
func PrepareCarCreate(c *models.Car) error {
	t := true
	c.IsVisible = &t
	if len(c.RentalModes) == 0 {
		c.RentalModes = []string{models.CarModeWithDriver, models.CarModeSelfDrive}
	}
	if !validModes(c.RentalModes) {
		return apierr.BadRequest(errInvalidCarFields)
	}
	if c.SelfDriveFrom != nil {
		f := dateOnly(*c.SelfDriveFrom)
		c.SelfDriveFrom = &f
	}
	if c.SelfDriveTo != nil {
		v := dateOnly(*c.SelfDriveTo)
		c.SelfDriveTo = &v
	}
	if !validRange(c.SelfDriveFrom, c.SelfDriveTo) {
		return apierr.BadRequest(errInvalidCarFields)
	}
	if !hasMode(c.RentalModes, models.CarModeSelfDrive) {
		c.SelfDriveFrom, c.SelfDriveTo = nil, nil
	}
	return nil
}

// parseModes converts an update value into a validated, non-empty mode list.
func parseModes(v interface{}) ([]string, bool) {
	var out []string
	switch s := v.(type) {
	case []string:
		out = append(out, s...)
	case []interface{}:
		for _, e := range s {
			str, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, str)
		}
	default:
		return nil, false
	}
	if len(out) == 0 || !validModes(out) {
		return nil, false
	}
	return out, true
}

// parseDate converts an update value into a date-only UTC time. nil and ""
// clear (nil result). Strings are "2006-01-02" or RFC3339; an RFC3339 value
// keeps the calendar date written in its own offset.
func parseDate(v interface{}) (*time.Time, bool) {
	switch s := v.(type) {
	case nil:
		return nil, true
	case time.Time:
		t := dateOnly(s)
		return &t, true
	case *time.Time:
		if s == nil {
			return nil, true
		}
		t := dateOnly(*s)
		return &t, true
	case string:
		if s == "" {
			return nil, true
		}
		if t, err := time.Parse("2006-01-02", s); err == nil {
			d := dateOnly(t)
			return &d, true
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			d := dateOnly(t) // literal date in the string's own offset
			return &d, true
		}
	}
	return nil, false
}

// NormalizeCarUpdate validates a partial car update against the stored car and
// rewrites it in place. Keys absent from update stay absent, except that
// dropping self_drive from the modes also clears the stored range.
func NormalizeCarUpdate(existing *models.Car, update bson.M) error {
	bad := apierr.BadRequest(errInvalidCarFields)

	if v, ok := update["is_visible"]; ok {
		if _, isBool := v.(bool); !isBool {
			return bad
		}
	}

	modes := CarModes(existing)
	if v, ok := update["rental_modes"]; ok {
		parsed, ok := parseModes(v)
		if !ok {
			return bad
		}
		update["rental_modes"] = parsed
		modes = parsed
	}

	from, to := existing.SelfDriveFrom, existing.SelfDriveTo
	_, hasFrom := update["self_drive_from"]
	_, hasTo := update["self_drive_to"]
	if hasFrom {
		f, ok := parseDate(update["self_drive_from"])
		if !ok {
			return bad
		}
		from = f
		if f == nil {
			update["self_drive_from"] = nil
		} else {
			update["self_drive_from"] = *f
		}
	}
	if hasTo {
		t, ok := parseDate(update["self_drive_to"])
		if !ok {
			return bad
		}
		to = t
		if t == nil {
			update["self_drive_to"] = nil
		} else {
			update["self_drive_to"] = *t
		}
	}

	if !hasMode(modes, models.CarModeSelfDrive) {
		if hasFrom || hasTo || from != nil || to != nil {
			update["self_drive_from"] = nil
			update["self_drive_to"] = nil
		}
		return nil
	}
	if (hasFrom || hasTo) && !validRange(from, to) {
		return bad
	}
	return nil
}

// ValidateRentalForCar checks a rental request against the car's availability,
// offered modes and self-drive date window (inclusive, date-only UTC).
func ValidateRentalForCar(c *models.Car, rt *models.Rental) error {
	if !c.IsActive || !CarVisible(c) {
		return apierr.NotFound("car")
	}
	if !hasMode(CarModes(c), string(rt.Mode)) {
		return apierr.BadRequest("rental mode not available")
	}
	pickup, ret := utcDateOnly(rt.PickupDate), utcDateOnly(rt.ReturnDate)
	if ret.Before(pickup) {
		return apierr.BadRequest("rental dates not available")
	}
	if rt.Mode == models.RentalSelfDrive && c.SelfDriveFrom != nil && c.SelfDriveTo != nil {
		if pickup.Before(dateOnly(*c.SelfDriveFrom)) || ret.After(dateOnly(*c.SelfDriveTo)) {
			return apierr.BadRequest("rental dates not available")
		}
	}
	return nil
}
