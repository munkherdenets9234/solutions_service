package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type RentalService struct {
	repo           *repository.RentalRepo
	customerRepo   *repository.CustomerRepo
	carRepo        *repository.CarRepo
	tenantUserRepo *repository.TenantUserRepo
	// notifier is nil when staff mail is off.
	notifier requestNotifier
}

// WithNotifier sets the staff-mail notifier. Pass an untyped nil to turn it off.
func (s *RentalService) WithNotifier(n requestNotifier) { s.notifier = n }

func NewRentalService(repo *repository.RentalRepo, customerRepo *repository.CustomerRepo, carRepo *repository.CarRepo, tenantUserRepo *repository.TenantUserRepo) *RentalService {
	return &RentalService{repo: repo, customerRepo: customerRepo, carRepo: carRepo, tenantUserRepo: tenantUserRepo}
}

type CreateRentalInput struct {
	CarID    string
	Customer models.Customer
	Rental   models.Rental
}

// RentalDetail is a rental enriched with its customer's details for API responses.
type RentalDetail struct {
	models.Rental
	Customer *models.Customer `json:"customer,omitempty"`
}

func (s *RentalService) Create(ctx context.Context, tenantID primitive.ObjectID, input CreateRentalInput) (*RentalDetail, error) {
	carID, err := primitive.ObjectIDFromHex(input.CarID)
	if err != nil {
		return nil, apierr.BadRequest("invalid car id")
	}

	car, err := s.carRepo.FindByID(ctx, tenantID, carID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("car not found")
		}
		return nil, apierr.Internal(err)
	}
	// Validate before the customer upsert so a rejected rental creates nothing.
	if err := ValidateRentalForCar(car, &input.Rental); err != nil {
		return nil, err
	}

	customer, err := s.customerRepo.Upsert(ctx, tenantID, &input.Customer)
	if err != nil {
		return nil, apierr.Internal(err)
	}

	rt := input.Rental
	rt.CarID = carID
	rt.CustomerID = customer.ID
	rt.ConfirmationID = uuid.NewString()

	if err := s.repo.Create(ctx, tenantID, &rt); err != nil {
		return nil, apierr.Internal(err)
	}
	if s.notifier != nil {
		s.notifier.Notify(ctx, tenantID, NotifyRental, rt.ID.Hex(), notifySummary(input.Customer.Name, "pickup", rt.PickupDate))
	}
	return &RentalDetail{Rental: rt, Customer: customer}, nil
}

func (s *RentalService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*RentalDetail, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rentals, total, err := s.repo.FindAll(ctx, tenantID, bson.M{}, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, rentals); err != nil {
		return nil, 0, apierr.Internal(err)
	}

	ids := make([]primitive.ObjectID, 0, len(rentals))
	seen := make(map[primitive.ObjectID]bool, len(rentals))
	for _, rt := range rentals {
		if !seen[rt.CustomerID] {
			seen[rt.CustomerID] = true
			ids = append(ids, rt.CustomerID)
		}
	}
	customers, err := s.customerRepo.FindByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	byID := make(map[primitive.ObjectID]*models.Customer, len(customers))
	for _, c := range customers {
		byID[c.ID] = c
	}

	details := make([]*RentalDetail, len(rentals))
	for i, rt := range rentals {
		details[i] = &RentalDetail{Rental: *rt, Customer: byID[rt.CustomerID]}
	}
	return details, total, nil
}

func (s *RentalService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*RentalDetail, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	rt, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("rental not found")
		}
		return nil, apierr.Internal(err)
	}

	customer, err := s.customerRepo.FindByID(ctx, tenantID, rt.CustomerID)
	if err != nil && err != mongo.ErrNoDocuments {
		return nil, apierr.Internal(err)
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, []*models.Rental{rt}); err != nil {
		return nil, apierr.Internal(err)
	}
	return &RentalDetail{Rental: *rt, Customer: customer}, nil
}

// resolveLastEditedBy populates each rental's LastEditedBy with the display
// name of the tenant user referenced by its UserID, for admin GET responses.
func (s *RentalService) resolveLastEditedBy(ctx context.Context, tenantID primitive.ObjectID, rentals []*models.Rental) error {
	ids := make([]primitive.ObjectID, 0, len(rentals))
	seen := make(map[primitive.ObjectID]bool, len(rentals))
	for _, rt := range rentals {
		if rt.UserID != nil && !seen[*rt.UserID] {
			seen[*rt.UserID] = true
			ids = append(ids, *rt.UserID)
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

	for _, rt := range rentals {
		if rt.UserID == nil {
			continue
		}
		if name, ok := names[*rt.UserID]; ok {
			rt.LastEditedBy = &name
		}
	}
	return nil
}

func (s *RentalService) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.RentalStatus, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, tenantID, id, status, userID)
}
