package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// maxRelatedRecords caps how many bookings/rentals/transfers are pulled into
// a customer's detail view - enough for any real customer's history without
// an unbounded query.
const maxRelatedRecords = 500

type CustomerService struct {
	repo           *repository.CustomerRepo
	bookingRepo    *repository.BookingRepo
	rentalRepo     *repository.RentalRepo
	transferRepo   *repository.AirportTransferRepo
	tenantUserRepo *repository.TenantUserRepo

	// creator and uploader back CreateManual. They are narrow interfaces so
	// the manual-create path is testable without MongoDB or Cloudinary.
	creator  customerCreator
	uploader avatarUploader
}

type customerCreator interface {
	Create(ctx context.Context, tenantID primitive.ObjectID, c *models.Customer) error
}

type avatarUploader interface {
	Upload(ctx context.Context, file io.Reader, tenantID primitive.ObjectID) (*UploadResult, error)
	Available() bool
}

// Field limits for an admin-entered customer.
const (
	maxCustomerNameRunes        = 200
	maxCustomerEmailRunes       = 254
	maxCustomerPhoneRunes       = 64
	maxCustomerNationalityRunes = 100
)

func NewCustomerService(repo *repository.CustomerRepo, bookingRepo *repository.BookingRepo, rentalRepo *repository.RentalRepo, transferRepo *repository.AirportTransferRepo, tenantUserRepo *repository.TenantUserRepo) *CustomerService {
	return &CustomerService{repo: repo, bookingRepo: bookingRepo, rentalRepo: rentalRepo, transferRepo: transferRepo, tenantUserRepo: tenantUserRepo, creator: repo}
}

// WithAvatarUploader enables avatar uploads for CreateManual. A nil
// *UploadService is fine: it reports itself unavailable.
func (s *CustomerService) WithAvatarUploader(u *UploadService) *CustomerService {
	s.uploader = u
	return s
}

// CreateManual creates a NEW customer from an admin form, optionally with an
// avatar. The avatar is accepted only if its leading bytes sniff as jpeg or
// png; the client's declared Content-Type is never consulted. Validation and
// sniffing both run before anything is uploaded or stored.
//
// If the insert fails after a successful upload, the uploaded image is left
// orphaned at the image host (accepted; there is no row to point at it).
func (s *CustomerService) CreateManual(ctx context.Context, tenantID primitive.ObjectID, c *models.Customer, avatar io.Reader, userID *primitive.ObjectID) (*models.Customer, error) {
	name := strings.TrimSpace(c.Name)
	email := strings.TrimSpace(c.Email)
	phone := strings.TrimSpace(c.Phone)
	nationality := strings.TrimSpace(c.Nationality)
	switch {
	case name == "":
		return nil, apierr.ValidationFailed("name is required")
	case utf8.RuneCountInString(name) > maxCustomerNameRunes:
		return nil, apierr.ValidationFailed("name is too long")
	case utf8.RuneCountInString(email) > maxCustomerEmailRunes:
		return nil, apierr.ValidationFailed("email is too long")
	case utf8.RuneCountInString(phone) > maxCustomerPhoneRunes:
		return nil, apierr.ValidationFailed("phone is too long")
	case utf8.RuneCountInString(nationality) > maxCustomerNationalityRunes:
		return nil, apierr.ValidationFailed("nationality is too long")
	}
	if email != "" {
		// ParseAddress also accepts "Name <a@b>"; require the bare address.
		addr, err := mail.ParseAddress(email)
		if err != nil || addr.Address != email {
			return nil, apierr.ValidationFailed("email is not valid")
		}
	}

	var head []byte
	if avatar != nil {
		buf := make([]byte, 512)
		n, err := io.ReadFull(avatar, buf)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, apierr.BadRequest("could not read avatar")
		}
		head = buf[:n]
		switch stripCharset(http.DetectContentType(head)) {
		case "image/jpeg", "image/png":
		default:
			return nil, apierr.ValidationFailed("avatar must be a jpeg or png image")
		}
		if s.uploader == nil || !s.uploader.Available() {
			return nil, apierr.FeatureUnavailable("image uploads")
		}
	}

	cust := &models.Customer{
		Name:        name,
		Email:       email,
		Phone:       phone,
		Nationality: nationality,
		UserID:      userID,
	}
	if avatar != nil {
		res, err := s.uploader.Upload(ctx, io.MultiReader(bytes.NewReader(head), avatar), tenantID)
		if err != nil {
			return nil, err
		}
		cust.AvatarURL = res.URL
	}
	if err := s.creator.Create(ctx, tenantID, cust); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, apierr.Conflict("a customer with this email already exists")
		}
		return nil, apierr.Internal(err)
	}
	return cust, nil
}

// CustomerSummary is a customer with counts of its related records, for the
// list view.
type CustomerSummary struct {
	models.Customer
	BookingCount         int64 `json:"booking_count"`
	RentalCount          int64 `json:"rental_count"`
	AirportTransferCount int64 `json:"airport_transfer_count"`
}

// CustomerDetail is a customer with its full related records, for the detail
// view.
type CustomerDetail struct {
	models.Customer
	Bookings         []*models.Booking         `json:"bookings"`
	Rentals          []*models.Rental          `json:"rentals"`
	AirportTransfers []*models.AirportTransfer `json:"airport_transfers"`
}

func (s *CustomerService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*CustomerSummary, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	customers, total, err := s.repo.FindAll(ctx, tenantID, page, limit)
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}

	ids := make([]primitive.ObjectID, len(customers))
	for i, c := range customers {
		ids[i] = c.ID
	}

	bookingCounts, err := s.bookingRepo.CountByCustomerIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	rentalCounts, err := s.rentalRepo.CountByCustomerIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	transferCounts, err := s.transferRepo.CountByCustomerIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, customers); err != nil {
		return nil, 0, apierr.Internal(err)
	}

	summaries := make([]*CustomerSummary, len(customers))
	for i, c := range customers {
		summaries[i] = &CustomerSummary{
			Customer:             *c,
			BookingCount:         bookingCounts[c.ID],
			RentalCount:          rentalCounts[c.ID],
			AirportTransferCount: transferCounts[c.ID],
		}
	}
	return summaries, total, nil
}

func (s *CustomerService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*CustomerDetail, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}

	c, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("customer not found")
		}
		return nil, apierr.Internal(err)
	}

	bookings, _, err := s.bookingRepo.FindAll(ctx, tenantID, bson.M{"customer_id": id}, 1, maxRelatedRecords)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	rentals, _, err := s.rentalRepo.FindAll(ctx, tenantID, bson.M{"customer_id": id}, 1, maxRelatedRecords)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	transfers, _, err := s.transferRepo.FindAll(ctx, tenantID, bson.M{"customer_id": id}, 1, maxRelatedRecords)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, []*models.Customer{c}); err != nil {
		return nil, apierr.Internal(err)
	}

	return &CustomerDetail{
		Customer:         *c,
		Bookings:         bookings,
		Rentals:          rentals,
		AirportTransfers: transfers,
	}, nil
}

// resolveLastEditedBy populates each customer's LastEditedBy with the display
// name of the tenant user referenced by its UserID, for admin GET responses.
func (s *CustomerService) resolveLastEditedBy(ctx context.Context, tenantID primitive.ObjectID, customers []*models.Customer) error {
	ids := make([]primitive.ObjectID, 0, len(customers))
	seen := make(map[primitive.ObjectID]bool, len(customers))
	for _, c := range customers {
		if c.UserID != nil && !seen[*c.UserID] {
			seen[*c.UserID] = true
			ids = append(ids, *c.UserID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	users, err := s.tenantUserRepo.FindByIDs(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	names := make(map[primitive.ObjectID]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Name
	}

	for _, c := range customers {
		if c.UserID == nil {
			continue
		}
		if name, ok := names[*c.UserID]; ok {
			c.LastEditedBy = &name
		}
	}
	return nil
}
