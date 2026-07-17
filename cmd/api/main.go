package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/handler"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/logger"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

func main() {
	_ = godotenv.Load()

	cfg := config.Load()
	logger.Init(cfg.AppEnv)
	defer logger.Sync()

	// MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		logger.Log.Fatal("mongo connect failed", zap.Error(err))
	}
	if err := client.Ping(ctx, nil); err != nil {
		logger.Log.Fatal("mongo ping failed", zap.Error(err))
	}
	logger.Log.Info("mongodb connected")
	db := client.Database(cfg.MongoDB)

	if err := repository.EnsureIndexes(ctx, db); err != nil {
		logger.Log.Fatal("index setup failed", zap.Error(err))
	}

	// Token maker
	tokenMaker, err := token.NewMaker(cfg.TokenSecret)
	if err != nil {
		logger.Log.Fatal("token maker failed", zap.Error(err))
	}

	// Wire up layers
	destRepo := repository.NewDestinationRepo(db)
	bookingRepo := repository.NewBookingRepo(db)
	blogRepo := repository.NewBlogRepo(db)
	customerRepo := repository.NewCustomerRepo(db)
	carRepo := repository.NewCarRepo(db)
	rentalRepo := repository.NewRentalRepo(db)
	airportTransferRepo := repository.NewAirportTransferRepo(db)
	contactMessageRepo := repository.NewContactMessageRepo(db)
	reviewRepo := repository.NewReviewRepo(db)
	partnerRepo := repository.NewPartnerRepo(db)
	tenantRepo := repository.NewTenantRepo(db)
	tenantUserRepo := repository.NewTenantUserRepo(db)
	platformUserRepo := repository.NewPlatformUserRepo(db)
	subscriptionRepo := repository.NewSubscriptionRepo(db)

	destSvc := service.NewDestinationService(destRepo, tenantUserRepo)
	bookingSvc := service.NewBookingService(bookingRepo, customerRepo, destRepo, tenantUserRepo)
	blogSvc := service.NewBlogService(blogRepo, tenantUserRepo)
	carSvc := service.NewCarService(carRepo)
	rentalSvc := service.NewRentalService(rentalRepo, customerRepo, carRepo, tenantUserRepo)
	airportTransferSvc := service.NewAirportTransferService(airportTransferRepo, customerRepo, tenantUserRepo)
	contactMessageSvc := service.NewContactMessageService(contactMessageRepo, tenantUserRepo)
	customerSvc := service.NewCustomerService(customerRepo, bookingRepo, rentalRepo, airportTransferRepo, tenantUserRepo)
	reviewSvc := service.NewReviewService(reviewRepo, tenantUserRepo)
	partnerSvc := service.NewPartnerService(partnerRepo, tenantUserRepo)
	tenantSvc := service.NewTenantService(tenantRepo)
	tenantUserSvc := service.NewTenantUserService(tenantUserRepo, tokenMaker, cfg.TokenExpiry)
	platformUserSvc := service.NewPlatformUserService(platformUserRepo, tokenMaker, cfg.TokenExpiry)
	subscriptionSvc := service.NewSubscriptionService(subscriptionRepo, platformUserRepo)
	uploadSvc, err := service.NewUploadService(cfg.CloudinaryURL)
	if err != nil {
		logger.Log.Fatal("upload service init failed", zap.Error(err))
	}

	if err := platformUserSvc.EnsureBootstrap(ctx, cfg.SuperadminName, cfg.SuperadminEmail, cfg.SuperadminPassword); err != nil {
		logger.Log.Fatal("superadmin bootstrap failed", zap.Error(err))
	}

	destHandler := handler.NewDestinationHandler(destSvc)
	bookingHandler := handler.NewBookingHandler(bookingSvc)
	blogHandler := handler.NewBlogHandler(blogSvc)
	carHandler := handler.NewCarHandler(carSvc)
	rentalHandler := handler.NewRentalHandler(rentalSvc)
	airportTransferHandler := handler.NewAirportTransferHandler(airportTransferSvc)
	contactMessageHandler := handler.NewContactMessageHandler(contactMessageSvc)
	customerHandler := handler.NewCustomerHandler(customerSvc)
	reviewHandler := handler.NewReviewHandler(reviewSvc)
	partnerHandler := handler.NewPartnerHandler(partnerSvc)
	tenantHandler := handler.NewTenantHandler(tenantSvc, tenantUserSvc)
	tenantUserHandler := handler.NewTenantUserHandler(tenantUserSvc)
	platformUserHandler := handler.NewPlatformUserHandler(platformUserSvc)
	subscriptionHandler := handler.NewSubscriptionHandler(subscriptionSvc)
	uploadHandler := handler.NewUploadHandler(uploadSvc)
	authMW := middleware.NewAuthMiddleware(tokenMaker)
	tenantMW := middleware.NewTenantMiddleware(tenantSvc)
	subscriptionMW := middleware.NewSubscriptionMiddleware(subscriptionSvc)

	router := handler.NewRouter(
		destHandler,
		bookingHandler,
		blogHandler,
		carHandler,
		rentalHandler,
		airportTransferHandler,
		contactMessageHandler,
		customerHandler,
		reviewHandler,
		partnerHandler,
		tenantHandler,
		tenantUserHandler,
		platformUserHandler,
		subscriptionHandler,
		uploadHandler,
		authMW,
		tenantMW,
		subscriptionMW,
	)

	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.Use(gin.Recovery())
	router.Register(engine)

	srv := &http.Server{
		Addr:         ":" + cfg.AppPort,
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		logger.Log.Info("server starting", zap.String("port", cfg.AppPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Fatal("server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Log.Info("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = client.Disconnect(shutdownCtx)
	logger.Log.Info("server stopped")
}
