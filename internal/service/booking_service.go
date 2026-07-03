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

func (s *BookingService) Create(ctx context.Context, tenantID primitive.ObjectID, input CreateBookingInput) (*models.Booking, error) {
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
	return &b, nil
}

func (s *BookingService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.Booking, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.FindAll(ctx, tenantID, bson.M{}, page, limit)
}

func (s *BookingService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.Booking, error) {
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
	return b, nil
}

func (s *BookingService) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.BookingStatus) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, tenantID, id, status)
}
