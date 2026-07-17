package service

import (
	"context"

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
}

func NewCustomerService(repo *repository.CustomerRepo, bookingRepo *repository.BookingRepo, rentalRepo *repository.RentalRepo, transferRepo *repository.AirportTransferRepo, tenantUserRepo *repository.TenantUserRepo) *CustomerService {
	return &CustomerService{repo: repo, bookingRepo: bookingRepo, rentalRepo: rentalRepo, transferRepo: transferRepo, tenantUserRepo: tenantUserRepo}
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
		return nil, 0, apierr.Internal()
	}

	ids := make([]primitive.ObjectID, len(customers))
	for i, c := range customers {
		ids[i] = c.ID
	}

	bookingCounts, err := s.bookingRepo.CountByCustomerIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, 0, apierr.Internal()
	}
	rentalCounts, err := s.rentalRepo.CountByCustomerIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, 0, apierr.Internal()
	}
	transferCounts, err := s.transferRepo.CountByCustomerIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, 0, apierr.Internal()
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, customers); err != nil {
		return nil, 0, apierr.Internal()
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
		return nil, apierr.Internal()
	}

	bookings, _, err := s.bookingRepo.FindAll(ctx, tenantID, bson.M{"customer_id": id}, 1, maxRelatedRecords)
	if err != nil {
		return nil, apierr.Internal()
	}
	rentals, _, err := s.rentalRepo.FindAll(ctx, tenantID, bson.M{"customer_id": id}, 1, maxRelatedRecords)
	if err != nil {
		return nil, apierr.Internal()
	}
	transfers, _, err := s.transferRepo.FindAll(ctx, tenantID, bson.M{"customer_id": id}, 1, maxRelatedRecords)
	if err != nil {
		return nil, apierr.Internal()
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, []*models.Customer{c}); err != nil {
		return nil, apierr.Internal()
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
