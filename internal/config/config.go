package config

import (
	"os"
	"strconv"
)

type Config struct {
	AppEnv        string
	AppPort       string
	MongoURI      string
	MongoDB       string
	RedisAddr     string
	RedisPass     string
	TokenSecret   string
	TokenExpiry   int // hours
	CloudinaryURL string

	SuperadminName     string
	SuperadminEmail    string
	SuperadminPassword string
}

func Load() *Config {
	return &Config{
		AppEnv: getEnv("APP_ENV", "development"),
		// PORT takes precedence over APP_PORT because hosting platforms like
		// Render inject PORT and require the app to bind to it.
		AppPort:       getEnv("PORT", getEnv("APP_PORT", "8080")),
		MongoURI:      getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:       getEnv("MONGO_DB", "innosolutions"),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPass:     getEnv("REDIS_PASS", ""),
		TokenSecret:   getEnv("TOKEN_SECRET", ""),
		TokenExpiry:   getEnvInt("TOKEN_EXPIRY_HOURS", 24),
		CloudinaryURL: getEnv("CLOUDINARY_URL", ""),

		SuperadminName:     getEnv("SUPERADMIN_NAME", ""),
		SuperadminEmail:    getEnv("SUPERADMIN_EMAIL", ""),
		SuperadminPassword: getEnv("SUPERADMIN_PASSWORD", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
