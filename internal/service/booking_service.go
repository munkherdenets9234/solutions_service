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

type BookingService struct {
	repo         *repository.BookingRepo
	customerRepo *repository.CustomerRepo
	destRepo     *repository.DestinationRepo
}

func NewBookingService(repo *repository.BookingRepo, customerRepo *repository.CustomerRepo, destRepo *repository.DestinationRepo) *BookingService {
	return &BookingService{repo: repo, customerRepo: customerRepo, destRepo: destRepo}
}

type CreateBookingInput struct {
	DestinationID string
	Customer      models.Customer
	Booking       models.Booking
}

// BookingDetail is a booking enriched with its customer's details for API responses.
type BookingDetail struct {
	models.Booking
	Customer *models.Customer `json:"customer,omitempty"`
}

func (s *BookingService) Create(ctx context.Context, tenantID primitive.ObjectID, input CreateBookingInput) (*BookingDetail, error) {
	destID, err := primitive.ObjectIDFromHex(input.DestinationID)
	if err != nil {
		return nil, apierr.BadRequest("invalid destination id")
	}

	if _, err := s.destRepo.FindByID(ctx, tenantID, destID); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("destination not found")
		}
		return nil, apierr.Internal()
	}

	customer, err := s.customerRepo.Upsert(ctx, tenantID, &input.Customer)
	if err != nil {
		return nil, apierr.Internal()
	}

	b := input.Booking
	b.DestinationID = destID
	b.CustomerID = customer.ID

	if err := s.repo.Create(ctx, tenantID, &b); err != nil {
		return nil, apierr.Internal()
	}
	return &BookingDetail{Booking: b, Customer: customer}, nil
}

func (s *BookingService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*BookingDetail, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	bookings, total, err := s.repo.FindAll(ctx, tenantID, bson.M{}, page, limit)
	if err != nil {
		return nil, 0, err
	}

	details, err := s.attachCustomers(ctx, tenantID, bookings)
	if err != nil {
		return nil, 0, err
	}
	return details, total, nil
}

func (s *BookingService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*BookingDetail, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	b, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("booking not found")
		}
		return nil, apierr.Internal()
	}

	customer, err := s.customerRepo.FindByID(ctx, tenantID, b.CustomerID)
	if err != nil && err != mongo.ErrNoDocuments {
		return nil, apierr.Internal()
	}
	return &BookingDetail{Booking: *b, Customer: customer}, nil
}

func (s *BookingService) attachCustomers(ctx context.Context, tenantID primitive.ObjectID, bookings []*models.Booking) ([]*BookingDetail, error) {
	ids := make([]primitive.ObjectID, 0, len(bookings))
	seen := make(map[primitive.ObjectID]bool, len(bookings))
	for _, b := range bookings {
		if !seen[b.CustomerID] {
			seen[b.CustomerID] = true
			ids = append(ids, b.CustomerID)
		}
	}

	customers, err := s.customerRepo.FindByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, apierr.Internal()
	}
	byID := make(map[primitive.ObjectID]*models.Customer, len(customers))
	for _, c := range customers {
		byID[c.ID] = c
	}

	details := make([]*BookingDetail, len(bookings))
	for i, b := range bookings {
		details[i] = &BookingDetail{Booking: *b, Customer: byID[b.CustomerID]}
	}
	return details, nil
}

func (s *BookingService) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.BookingStatus) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, tenantID, id, status)
}
