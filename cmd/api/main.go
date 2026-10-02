// Command api is the service entry point. It does four things and delegates
// everything else to internal/bootstrap: load the environment, start the
// logger, wire the app, run it.
package main

import (
	"context"
	"os"

	"github.com/eandstravel/digitalservice/internal/bootstrap"
	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/pkg/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	_ = godotenv.Load()

	cfg := config.Load()
	logger.Init(string(cfg.AppEnv))
	defer logger.Sync()

	app, err := bootstrap.New(context.Background(), cfg)
	if err != nil {
		// Configuration problems are reported as one list rather than one
		// restart at a time (see config.Validate), so this line is usually
		// the only thing an operator needs to fix a fresh environment.
		logger.Log.Error("startup failed", zap.Error(err))
		_ = logger.Log.Sync()
		os.Exit(1)
	}

	if err := app.Run(); err != nil {
		logger.Log.Error("server stopped unexpectedly", zap.Error(err))
		_ = logger.Log.Sync()
		os.Exit(1)
	}
}
