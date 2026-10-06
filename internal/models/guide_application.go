package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type GuideStatus string

const (
	GuideNew         GuideStatus = "new"
	GuideReviewing   GuideStatus = "reviewing"
	GuideShortlisted GuideStatus = "shortlisted"
	GuideRejected    GuideStatus = "rejected"
	GuideHired       GuideStatus = "hired"
)

type GuideFileKind string

const (
	GuideFilePhoto            GuideFileKind = "photo"
	GuideFileIDCard           GuideFileKind = "id_card"
	GuideFileDriverLicense    GuideFileKind = "driver_license"
	GuideFileGuideCertificate GuideFileKind = "guide_certificate"
	GuideFileCV               GuideFileKind = "cv"
	GuideFileFirstAid         GuideFileKind = "first_aid"
)

// Allowed values, from the guide recruitment spec. The service layer validates
// against these.
var (
	GuideStatuses = []GuideStatus{GuideNew, GuideReviewing, GuideShortlisted, GuideRejected, GuideHired}

	GuideFileKinds = []GuideFileKind{
		GuideFilePhoto, GuideFileIDCard, GuideFileDriverLicense,
		GuideFileGuideCertificate, GuideFileCV, GuideFileFirstAid,
	}

	GuideRegions = []string{"gobi", "central", "khuvsgul", "western", "eastern", "ulaanbaatar_terelj", "other"}

	GuideTourTypes = []string{
		"private", "group", "vip", "adventure_4x4", "cultural",
		"hiking_trekking", "festival", "business_corporate",
	}

	GuideTripLengths = []string{"d1_3", "d4_7", "d8_14", "d15_plus"}

	GuideLanguageCodes = []string{"mn", "en", "ko", "zh", "ja", "ru", "fr", "es", "other"}
)

type GuideEmergencyContact struct {
	Name  string `bson:"name" json:"name"`
	Phone string `bson:"phone" json:"phone"`
}

type GuidePersonal struct {
	FullName         string                `bson:"full_name" json:"full_name"`
	Nickname         string                `bson:"nickname" json:"nickname"`
	BirthDate        time.Time             `bson:"birth_date" json:"birth_date"`
	Gender           string                `bson:"gender" json:"gender"` // male|female|other|undisclosed
	Phone            string                `bson:"phone" json:"phone"`
	Email            string                `bson:"email" json:"email"`
	Address          string                `bson:"address" json:"address"`
	EmergencyContact GuideEmergencyContact `bson:"emergency_contact" json:"emergency_contact"`
}

type GuideLanguage struct {
	Language  string `bson:"language" json:"language"`
	OtherName string `bson:"other_name,omitempty" json:"other_name,omitempty"`
	Level     string `bson:"level" json:"level"`
}

type GuideExperience struct {
	Years             int      `bson:"years" json:"years"`
	PreviousCompanies string   `bson:"previous_companies" json:"previous_companies"`
	TourTypes         []string `bson:"tour_types" json:"tour_types"`
	MainDirections    string   `bson:"main_directions" json:"main_directions"`
	LargestGroup      int      `bson:"largest_group" json:"largest_group"`
}

type GuideDriving struct {
	HasLicense    bool   `bson:"has_license" json:"has_license"`
	LicenseClass  string `bson:"license_class" json:"license_class"`
	YearsDriving  int    `bson:"years_driving" json:"years_driving"`
	CanDrive4x4   bool   `bson:"can_drive_4x4" json:"can_drive_4x4"`
	LongDistance  bool   `bson:"long_distance" json:"long_distance"`
	HasOwnVehicle bool   `bson:"has_own_vehicle" json:"has_own_vehicle"`
	Vehicles      string `bson:"vehicles" json:"vehicles"`
}

type GuideAvailability struct {
	Months      []int    `bson:"months" json:"months"`
	Days        string   `bson:"days" json:"days"`
	TripLengths []string `bson:"trip_lengths" json:"trip_lengths"`
	FullSeason  bool     `bson:"full_season" json:"full_season"`
	BookedTrips string   `bson:"booked_trips" json:"booked_trips"`
}

type GuideReference struct {
	Name     string `bson:"name" json:"name"`
	Position string `bson:"position" json:"position"`
	Contact  string `bson:"contact" json:"contact"`
}

// GuideFile is metadata for a private Cloudinary file. PublicID is never meant
// for clients: staff open files through short-lived signed links.
type GuideFile struct {
	ID           string        `bson:"id" json:"id"`
	Kind         GuideFileKind `bson:"kind" json:"kind"`
	PublicID     string        `bson:"public_id" json:"-"`
	Mime         string        `bson:"mime" json:"mime"`
	Size         int64         `bson:"size" json:"size"`
	OriginalName string        `bson:"original_name" json:"original_name"`
}

// GuideEvent is an append-only timeline entry (Type is "status" or "note").
// UserName is stored at write time so the timeline survives a staff rename.
type GuideEvent struct {
	Type     string              `bson:"type" json:"type"`
	At       time.Time           `bson:"at" json:"at"`
	UserID   *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	UserName string              `bson:"user_name" json:"user_name"`
	From     string              `bson:"from,omitempty" json:"from,omitempty"`
	To       string              `bson:"to,omitempty" json:"to,omitempty"`
	Text     string              `bson:"text,omitempty" json:"text,omitempty"`
}

type GuideApplication struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID     primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Season       string             `bson:"season" json:"season"`
	Locale       string             `bson:"locale" json:"locale"`
	Personal     GuidePersonal      `bson:"personal" json:"personal"`
	Languages    []GuideLanguage    `bson:"languages" json:"languages"`
	Experience   GuideExperience    `bson:"experience" json:"experience"`
	Regions      []string           `bson:"regions" json:"regions"`
	RegionsOther string             `bson:"regions_other" json:"regions_other"`
	Driving      GuideDriving       `bson:"driving" json:"driving"`
	Availability GuideAvailability  `bson:"availability" json:"availability"`
	References   []GuideReference   `bson:"references" json:"references"`
	Files        []GuideFile        `bson:"files" json:"files"`
	Status       GuideStatus        `bson:"status" json:"status"`
	Events       []GuideEvent       `bson:"events" json:"events"`
	ConsentAt    time.Time          `bson:"consent_at" json:"consent_at"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}
