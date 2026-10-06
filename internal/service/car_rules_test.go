package service

import (
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/apierr"
)

func d(s string) *time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		panic(err)
	}
	return &t
}

func bp(b bool) *bool { return &b }

func isStatus(err error, code int) bool {
	ae, ok := err.(*apierr.APIError)
	return ok && ae.HTTPStatus == code
}

func TestCarVisible(t *testing.T) {
	tests := []struct {
		name string
		v    *bool
		want bool
	}{
		{"nil", nil, true},
		{"true", bp(true), true},
		{"false", bp(false), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CarVisible(&models.Car{IsVisible: tc.v}); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestCarModes(t *testing.T) {
	both := []string{models.CarModeWithDriver, models.CarModeSelfDrive}
	tests := []struct {
		name  string
		modes []string
		want  []string
	}{
		{"empty", nil, both},
		{"self only", []string{models.CarModeSelfDrive}, []string{models.CarModeSelfDrive}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CarModes(&models.Car{RentalModes: tc.modes})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestPrepareCarCreate(t *testing.T) {
	tests := []struct {
		name    string
		car     models.Car
		wantErr bool
		check   func(t *testing.T, c *models.Car)
	}{
		{"omitted visibility becomes visible", models.Car{}, false, func(t *testing.T, c *models.Car) {
			if c.IsVisible == nil || !*c.IsVisible {
				t.Fatal("expected visible")
			}
		}},
		{"empty modes default to both", models.Car{}, false, func(t *testing.T, c *models.Car) {
			if len(c.RentalModes) != 2 {
				t.Fatalf("modes %v", c.RentalModes)
			}
		}},
		{"unknown mode", models.Car{RentalModes: []string{"boat"}}, true, nil},
		{"only from", models.Car{SelfDriveFrom: d("2026-07-01")}, true, nil},
		{"from after to", models.Car{SelfDriveFrom: d("2026-08-01"), SelfDriveTo: d("2026-07-01")}, true, nil},
		{"range cleared without self drive", models.Car{
			RentalModes:   []string{models.CarModeWithDriver},
			SelfDriveFrom: d("2026-07-01"), SelfDriveTo: d("2026-08-01"),
		}, false, func(t *testing.T, c *models.Car) {
			if c.SelfDriveFrom != nil || c.SelfDriveTo != nil {
				t.Fatal("range should be cleared")
			}
		}},
		{"valid range kept", models.Car{SelfDriveFrom: d("2026-07-01"), SelfDriveTo: d("2026-08-01")}, false, func(t *testing.T, c *models.Car) {
			if c.SelfDriveFrom == nil || c.SelfDriveTo == nil {
				t.Fatal("range should be kept")
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.car
			err := PrepareCarCreate(&c)
			if tc.wantErr {
				if !isStatus(err, 400) {
					t.Fatalf("want 400, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, &c)
		})
	}
}

func TestNormalizeCarUpdate(t *testing.T) {
	ranged := &models.Car{SelfDriveFrom: d("2026-06-01"), SelfDriveTo: d("2026-09-30")}
	tests := []struct {
		name     string
		existing *models.Car
		update   bson.M
		wantErr  bool
		check    func(t *testing.T, u bson.M)
	}{
		{"visibility only leaves others absent", ranged, bson.M{"is_visible": false}, false, func(t *testing.T, u bson.M) {
			for _, k := range []string{"rental_modes", "self_drive_from", "self_drive_to"} {
				if _, ok := u[k]; ok {
					t.Fatalf("%s should be absent", k)
				}
			}
		}},
		{"without self drive clears range", ranged, bson.M{"rental_modes": []string{"with_driver"}}, false, func(t *testing.T, u bson.M) {
			for _, k := range []string{"self_drive_from", "self_drive_to"} {
				v, ok := u[k]
				if !ok || v != nil {
					t.Fatalf("%s should be nil, got %v (present %v)", k, v, ok)
				}
			}
		}},
		{"dates convert", &models.Car{}, bson.M{"self_drive_from": "2026-07-01", "self_drive_to": "2026-08-31"}, false, func(t *testing.T, u bson.M) {
			if u["self_drive_from"] != *d("2026-07-01") || u["self_drive_to"] != *d("2026-08-31") {
				t.Fatalf("got %v %v", u["self_drive_from"], u["self_drive_to"])
			}
		}},
		{"only from without stored to", &models.Car{}, bson.M{"self_drive_from": "2026-07-01"}, true, nil},
		{"to before from", &models.Car{}, bson.M{"self_drive_from": "2026-08-31", "self_drive_to": "2026-07-01"}, true, nil},
		{"visibility wrong type", &models.Car{}, bson.M{"is_visible": "yes"}, true, nil},
		{"empty modes", &models.Car{}, bson.M{"rental_modes": []string{}}, true, nil},
		{"unknown mode", &models.Car{}, bson.M{"rental_modes": []string{"boat"}}, true, nil},
		{"bad date", &models.Car{}, bson.M{"self_drive_from": "nope", "self_drive_to": "2026-07-01"}, true, nil},
		{"offset reduced to date", &models.Car{}, bson.M{"self_drive_from": "2026-07-01T23:30:00-05:00", "self_drive_to": "2026-08-31"}, false, func(t *testing.T, u bson.M) {
			if u["self_drive_from"] != *d("2026-07-01") {
				t.Fatalf("got %v", u["self_drive_from"])
			}
		}},
		{"clear range with empty values", ranged, bson.M{"self_drive_from": "", "self_drive_to": nil}, false, func(t *testing.T, u bson.M) {
			if u["self_drive_from"] != nil || u["self_drive_to"] != nil {
				t.Fatal("expected nil")
			}
		}},
		{"modes as interface slice", &models.Car{}, bson.M{"rental_modes": []interface{}{"self_drive"}}, false, func(t *testing.T, u bson.M) {
			if !reflect.DeepEqual(u["rental_modes"], []string{"self_drive"}) {
				t.Fatalf("got %v", u["rental_modes"])
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NormalizeCarUpdate(tc.existing, tc.update)
			if tc.wantErr {
				if !isStatus(err, 400) {
					t.Fatalf("want 400, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, tc.update)
		})
	}
}

func TestValidateRentalForCar(t *testing.T) {
	ranged := func() *models.Car {
		return &models.Car{IsActive: true, RentalModes: []string{models.CarModeSelfDrive},
			SelfDriveFrom: d("2026-07-01"), SelfDriveTo: d("2026-08-31")}
	}
	rental := func(mode models.RentalMode, p, r string) *models.Rental {
		return &models.Rental{Mode: mode, PickupDate: *d(p), ReturnDate: *d(r)}
	}
	hidden := ranged()
	hidden.IsVisible = bp(false)
	inactive := ranged()
	inactive.IsActive = false
	open := &models.Car{IsActive: true}

	tests := []struct {
		name string
		car  *models.Car
		rt   *models.Rental
		want int // 0 = nil
	}{
		{"inside", ranged(), rental(models.RentalSelfDrive, "2026-07-01", "2026-07-03"), 0},
		{"end boundary", ranged(), rental(models.RentalSelfDrive, "2026-08-30", "2026-08-31"), 0},
		{"pickup before", ranged(), rental(models.RentalSelfDrive, "2026-06-30", "2026-07-03"), 400},
		{"return after", ranged(), rental(models.RentalSelfDrive, "2026-08-30", "2026-09-01"), 400},
		{"return before pickup", ranged(), rental(models.RentalSelfDrive, "2026-07-05", "2026-07-03"), 400},
		{"mode not offered", ranged(), rental(models.RentalWithDriver, "2026-07-01", "2026-07-03"), 400},
		{"hidden", hidden, rental(models.RentalSelfDrive, "2026-07-01", "2026-07-03"), 404},
		{"inactive", inactive, rental(models.RentalSelfDrive, "2026-07-01", "2026-07-03"), 404},
		{"legacy car both modes any dates", open, rental(models.RentalSelfDrive, "2030-01-01", "2030-01-05"), 0},
		{"legacy car with driver", open, rental(models.RentalWithDriver, "2030-01-01", "2030-01-05"), 0},
		{"with driver ignores range", &models.Car{IsActive: true, SelfDriveFrom: d("2026-07-01"), SelfDriveTo: d("2026-08-31")},
			rental(models.RentalWithDriver, "2027-01-01", "2027-01-05"), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRentalForCar(tc.car, tc.rt)
			if tc.want == 0 {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if !isStatus(err, tc.want) {
				t.Fatalf("want %d, got %v", tc.want, err)
			}
		})
	}
}
