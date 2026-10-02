// Package config loads and validates every setting this service reads from
// the environment.
//
// Two rules shape it:
//
//   - A missing *required* setting stops the process, and Validate reports
//     every missing key at once. Discovering them one restart at a time is
//     the slowest possible way to bring up a new environment.
//   - A missing *optional* setting disables one feature and nothing else.
//     Which features those are is not implicit: Features() names them, the
//     startup log states each one's status, and /readyz reports it for as
//     long as the process runs.
package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// Env identifies the runtime environment.
type Env string

const (
	EnvDevelopment Env = "development"
	EnvProduction  Env = "production"
	EnvTest        Env = "test"
)

type Config struct {
	AppEnv  Env
	AppPort string

	// Required.
	MongoURI    string
	MongoDB     string
	TokenSecret string

	TokenExpiry int // hours

	// Optional — each blank value disables exactly one feature.
	CloudinaryURL      string // blank: image uploads unavailable
	SuperadminName     string
	SuperadminEmail    string // blank (with password): no startup superadmin bootstrap
	SuperadminPassword string

	// The tenantcore link. Subscriptions and plans live there, not here —
	// this service holds no subscriptions collection any more. Both blank:
	// entitlement is unenforced (see entitlement.Unenforced), which is a
	// misconfiguration in production and is reported as one on /readyz.
	TenantcoreURL        string
	TenantcoreServiceKey string
	// How long an entitlement is trusted, and how long a stale one may still
	// be served once tenantcore stops answering. The grace window is the
	// difference between a platform blip and an outage for every tenant.
	EntitlementTTLSeconds   int
	EntitlementGraceSeconds int
	EntitlementTimeoutMS    int

	// Uploads.
	UploadMaxBytes int64

	// Rate limiting. Per client IP, per named route group. See
	// middleware.RateLimiter for why these are per-instance.
	RateLimitEnabled  bool
	AuthRatePerMinute int // /login and password-change endpoints
	LeadRatePerMinute int // anonymous lead forms: contact, quote, newsletter, reviews
	RateLimitBurst    int
}

// IsDev reports whether stack traces and debug routing are appropriate.
// Anything that is not explicitly production is treated as development, so a
// blank or misspelled APP_ENV never accidentally hides diagnostics.
func (c Config) IsDev() bool { return c.AppEnv != EnvProduction }

// UploadsEnabled reports whether image uploads are configured.
func (c Config) UploadsEnabled() bool { return c.CloudinaryURL != "" }

// SuperadminBootstrapEnabled reports whether a first platform user should be
// created at startup when none exists.
func (c Config) SuperadminBootstrapEnabled() bool {
	return c.SuperadminEmail != "" && c.SuperadminPassword != ""
}

// EntitlementEnabled reports whether this deployment can ask tenantcore what
// a tenant has bought. Both halves are required: a URL with no service key
// gets 401 on every call, which is a worse failure than being switched off,
// because it looks like an outage rather than a missing setting.
func (c Config) EntitlementEnabled() bool {
	return c.TenantcoreURL != "" && c.TenantcoreServiceKey != ""
}

// PasswordResetEnabled reports whether a reset code can be delivered. The mail
// goes through tenantcore, so it needs the same two settings as the entitlement
// link; without them no code can ever arrive.
func (c Config) PasswordResetEnabled() bool {
	return c.TenantcoreURL != "" && c.TenantcoreServiceKey != ""
}

// Feature is one optional capability and whether this deployment has it.
type Feature struct {
	Name    string
	Enabled bool
	// Detail says what is missing when Enabled is false, in the terms an
	// operator can act on: the env var to set, not the symptom.
	Detail string
}

// Features is the full list of optional capabilities, in a fixed order. It is
// logged at startup and served by /readyz. This list is the contract: if a
// feature can be switched off by configuration, it appears here, so "the
// feature was quietly unmounted and nobody noticed" has one place to look.
func (c Config) Features() []Feature {
	return []Feature{
		{
			Name:    "uploads",
			Enabled: c.UploadsEnabled(),
			Detail:  "CLOUDINARY_URL is not set — POST /admin/uploads returns 503 FEATURE_UNAVAILABLE",
		},
		{
			Name:    "superadmin_bootstrap",
			Enabled: c.SuperadminBootstrapEnabled(),
			Detail:  "SUPERADMIN_EMAIL/SUPERADMIN_PASSWORD are not both set — no first platform user is created",
		},
		{
			Name:    "rate_limiting",
			Enabled: c.RateLimitEnabled,
			Detail:  "RATE_LIMIT_ENABLED=false — login and public lead forms accept unlimited requests",
		},
		{
			Name:    "entitlement",
			Enabled: c.EntitlementEnabled(),
			Detail: "TENANTCORE_URL/TENANTCORE_SERVICE_KEY are not both set — subscription and " +
				"module gates are UNENFORCED; every tenant is treated as unprovisioned",
		},
		{
			Name:    "password_reset",
			Enabled: c.PasswordResetEnabled(),
			Detail: "TENANTCORE_URL/TENANTCORE_SERVICE_KEY are not both set — a tenant user who forgets their " +
				"password cannot be sent a reset code, and POST /password-reset/request answers 503",
		},
	}
}

// Validate checks every required setting and reports all failures together.
func (c Config) Validate() error {
	var problems []string

	require := func(val, name string) {
		if strings.TrimSpace(val) == "" {
			problems = append(problems, name+" is required")
		}
	}

	require(c.MongoURI, "MONGO_URI")
	require(c.MongoDB, "MONGO_DB")
	require(c.AppPort, "PORT (or APP_PORT)")

	// Checked here rather than at token.NewMaker so a short secret is
	// reported alongside every other configuration problem, in one pass.
	if strings.TrimSpace(c.TokenSecret) == "" {
		problems = append(problems, "TOKEN_SECRET is required")
	} else if len(c.TokenSecret) < 32 {
		problems = append(problems, "TOKEN_SECRET must be at least 32 characters")
	}

	if c.TokenExpiry < 1 {
		problems = append(problems, "TOKEN_EXPIRY_HOURS must be at least 1")
	}

	// A bootstrap password that is set but unusable would fail silently at
	// startup, leaving a deployment with no way to log in and no message
	// saying why.
	if c.SuperadminEmail != "" && c.SuperadminPassword != "" && len(c.SuperadminPassword) < 8 {
		problems = append(problems, "SUPERADMIN_PASSWORD must be at least 8 characters")
	}

	if len(problems) > 0 {
		return errors.New("invalid configuration: " + strings.Join(problems, "; "))
	}
	return nil
}

func Load() *Config {
	return &Config{
		AppEnv: Env(getEnv("APP_ENV", string(EnvDevelopment))),
		// PORT takes precedence over APP_PORT because hosting platforms like
		// Render inject PORT and require the app to bind to it.
		AppPort:     getEnv("PORT", getEnv("APP_PORT", "8080")),
		MongoURI:    getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:     getEnv("MONGO_DB", "innosolutions"),
		TokenSecret: getEnv("TOKEN_SECRET", ""),
		TokenExpiry: getEnvInt("TOKEN_EXPIRY_HOURS", 24),

		CloudinaryURL: getEnv("CLOUDINARY_URL", ""),

		SuperadminName:     getEnv("SUPERADMIN_NAME", ""),
		SuperadminEmail:    getEnv("SUPERADMIN_EMAIL", ""),
		SuperadminPassword: getEnv("SUPERADMIN_PASSWORD", ""),

		TenantcoreURL:           getEnv("TENANTCORE_URL", ""),
		TenantcoreServiceKey:    getEnv("TENANTCORE_SERVICE_KEY", ""),
		EntitlementTTLSeconds:   getEnvInt("ENTITLEMENT_TTL_SECONDS", 60),
		EntitlementGraceSeconds: getEnvInt("ENTITLEMENT_GRACE_SECONDS", 900),
		EntitlementTimeoutMS:    getEnvInt("ENTITLEMENT_TIMEOUT_MS", 3000),

		UploadMaxBytes: int64(getEnvInt("UPLOAD_MAX_BYTES", 10<<20)), // 10 MiB

		RateLimitEnabled:  getEnvBool("RATE_LIMIT_ENABLED", true),
		AuthRatePerMinute: getEnvInt("AUTH_RATE_PER_MINUTE", 10),
		LeadRatePerMinute: getEnvInt("LEAD_RATE_PER_MINUTE", 20),
		RateLimitBurst:    getEnvInt("RATE_LIMIT_BURST", 5),
	}
}

// getEnv trims the value it reads. A .env file saved with CRLF line endings
// yields values like "mongodb://...\r", which look correct in an editor and
// fail every connection attempt with an error that names neither the file nor
// the character.
func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := getEnv(key, ""); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := getEnv(key, ""); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
