package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		AppEnv:      EnvProduction,
		AppPort:     "8080",
		MongoURI:    "mongodb://localhost:27017",
		MongoDB:     "app",
		TokenSecret: strings.Repeat("k", 32),
		TokenExpiry: 24,
	}
}

func TestValidateAcceptsAMinimalConfig(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("a config with only the required keys should be valid, got: %v", err)
	}
}

// TestValidateReportsEveryProblemAtOnce is the behaviour that matters. Fixing
// a fresh environment one restart per missing key is the slowest possible way
// to bring a deployment up.
func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	cfg := Config{AppEnv: EnvProduction}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("an empty config should not validate")
	}

	for _, want := range []string{"MONGO_URI", "MONGO_DB", "PORT", "TOKEN_SECRET", "TOKEN_EXPIRY_HOURS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %s; got: %v", want, err)
		}
	}
}

func TestValidateRejectsAShortTokenSecret(t *testing.T) {
	cfg := validConfig()
	cfg.TokenSecret = "too-short"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("a 9-character token secret should not validate")
	}
	if !strings.Contains(err.Error(), "32 characters") {
		t.Errorf("error should say what the minimum is; got: %v", err)
	}
}

// A bootstrap password that is set but unusable would fail at startup with
// nothing to say why, leaving a deployment nobody can log into.
func TestValidateRejectsAnUnusableSuperadminPassword(t *testing.T) {
	cfg := validConfig()
	cfg.SuperadminEmail = "admin@example.com"
	cfg.SuperadminPassword = "short"

	if err := cfg.Validate(); err == nil {
		t.Fatal("a 5-character superadmin password should not validate")
	}
}

func TestIsDevTreatsAnythingButProductionAsDevelopment(t *testing.T) {
	// A blank or misspelled APP_ENV must not silently disable diagnostics.
	for _, env := range []Env{EnvDevelopment, EnvTest, "", "prod", "Production"} {
		cfg := Config{AppEnv: env}
		if !cfg.IsDev() {
			t.Errorf("AppEnv=%q: IsDev() = false, want true", env)
		}
	}
	if (Config{AppEnv: EnvProduction}).IsDev() {
		t.Error("AppEnv=production: IsDev() = true, want false")
	}
}

// TestFeaturesNamesTheSettingToFix guards the property that makes a degraded
// deployment diagnosable: every disabled feature must say which environment
// variable turns it back on.
func TestFeaturesNamesTheSettingToFix(t *testing.T) {
	cfg := validConfig() // no Cloudinary, no superadmin, no rate limiting

	for _, f := range cfg.Features() {
		if f.Enabled {
			continue
		}
		if f.Detail == "" {
			t.Errorf("feature %q is disabled with no detail", f.Name)
			continue
		}
		if !strings.ContainsAny(f.Detail, "ABCDEFGHIJKLMNOPQRSTUVWXYZ_") {
			t.Errorf("feature %q detail should name an env var; got %q", f.Name, f.Detail)
		}
	}
}

func TestFeaturesTracksUploadConfiguration(t *testing.T) {
	cfg := validConfig()
	if cfg.UploadsEnabled() {
		t.Error("uploads should be disabled with no CLOUDINARY_URL")
	}

	cfg.CloudinaryURL = "cloudinary://key:secret@cloud"
	if !cfg.UploadsEnabled() {
		t.Error("uploads should be enabled once CLOUDINARY_URL is set")
	}

	for _, f := range cfg.Features() {
		if f.Name == "uploads" && !f.Enabled {
			t.Error("Features() disagrees with UploadsEnabled()")
		}
	}
}

func TestPrivateFilesURLFallsBackToCloudinaryURL(t *testing.T) {
	cfg := validConfig()
	if cfg.PrivateFilesEnabled() {
		t.Error("private files should be disabled with neither URL set")
	}

	cfg.CloudinaryURL = "cloudinary://k:s@testcloud"
	if got := cfg.PrivateFilesURL(); got != cfg.CloudinaryURL {
		t.Errorf("PrivateFilesURL() = %q, want fallback to CloudinaryURL", got)
	}
	if !cfg.PrivateFilesEnabled() {
		t.Error("private files should be enabled by CloudinaryURL alone")
	}

	cfg.CloudinaryPrivateURL = "cloudinary://k2:s2@privatecloud"
	if got := cfg.PrivateFilesURL(); got != cfg.CloudinaryPrivateURL {
		t.Errorf("PrivateFilesURL() = %q, want the private URL to win", got)
	}

	for _, f := range cfg.Features() {
		if f.Name == "private_files" && !f.Enabled {
			t.Error("Features() disagrees with PrivateFilesEnabled()")
		}
	}
}

// Bootstrapping needs BOTH halves. An email with no password used to be
// accepted and then quietly do nothing.
func TestSuperadminBootstrapNeedsBothHalves(t *testing.T) {
	cases := []struct {
		email, password string
		want            bool
	}{
		{"", "", false},
		{"admin@example.com", "", false},
		{"", "password123", false},
		{"admin@example.com", "password123", true},
	}
	for _, c := range cases {
		cfg := Config{SuperadminEmail: c.email, SuperadminPassword: c.password}
		if got := cfg.SuperadminBootstrapEnabled(); got != c.want {
			t.Errorf("email=%q password=%q: got %v, want %v", c.email, c.password, got, c.want)
		}
	}
}

// A .env file saved with CRLF endings yields values with a trailing \r that
// look correct in an editor and fail every connection attempt.
func TestGetEnvTrimsWhitespace(t *testing.T) {
	t.Setenv("TEST_TRIM_KEY", "  mongodb://host\r")
	if got := getEnv("TEST_TRIM_KEY", "fallback"); got != "mongodb://host" {
		t.Errorf("got %q, want %q", got, "mongodb://host")
	}
}

func TestGetEnvFallsBackOnBlank(t *testing.T) {
	t.Setenv("TEST_BLANK_KEY", "   ")
	if got := getEnv("TEST_BLANK_KEY", "fallback"); got != "fallback" {
		t.Errorf("a whitespace-only value should fall back; got %q", got)
	}
}

// Password reset sends its mail through tenantcore, so it needs the same two
// settings as the entitlement link. Either missing means no code can ever be
// delivered, and the readiness entry must say so by name.
func TestPasswordResetNeedsTheTenantcoreLink(t *testing.T) {
	cases := []struct {
		name string
		url  string
		key  string
		want bool
	}{
		{"both set", "http://localhost:8092", "svc-key", true},
		{"url only", "http://localhost:8092", "", false},
		{"key only", "", "svc-key", false},
		{"neither", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig()
			c.TenantcoreURL, c.TenantcoreServiceKey = tc.url, tc.key

			if got := c.PasswordResetEnabled(); got != tc.want {
				t.Fatalf("PasswordResetEnabled() = %v, want %v", got, tc.want)
			}

			var feature *Feature
			for _, f := range c.Features() {
				if f.Name == "password_reset" {
					f := f
					feature = &f
				}
			}
			if feature == nil {
				t.Fatal("Features() has no password_reset entry")
			}
			if feature.Enabled != tc.want {
				t.Fatalf("password_reset Enabled = %v, want %v", feature.Enabled, tc.want)
			}
			if !tc.want {
				for _, name := range []string{"TENANTCORE_URL", "TENANTCORE_SERVICE_KEY"} {
					if !strings.Contains(feature.Detail, name) {
						t.Fatalf("disabled detail should name %s, got %q", name, feature.Detail)
					}
				}
			}
		})
	}
}

func TestValidateTenantcorePublicKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	cfg := validConfig()
	cfg.TenantcorePublicKey = base64.StdEncoding.EncodeToString(pub)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a valid public key should pass, got: %v", err)
	}

	for name, bad := range map[string]string{
		"not base64":   "%%%not-base64",
		"wrong length": base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		cfg := validConfig()
		cfg.TenantcorePublicKey = bad
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "TENANTCORE_PUBLIC_KEY") {
			t.Errorf("%s: want an error naming TENANTCORE_PUBLIC_KEY, got: %v", name, err)
		}
	}
}

func TestFeaturesReportTenantcoreAdminUsers(t *testing.T) {
	find := func(c Config) Feature {
		for _, f := range c.Features() {
			if f.Name == "tenantcore_admin_users" {
				return f
			}
		}
		t.Fatal("Features() has no tenantcore_admin_users entry")
		return Feature{}
	}

	cfg := validConfig()
	if f := find(cfg); f.Enabled || !strings.Contains(f.Detail, "TENANTCORE_PUBLIC_KEY") {
		t.Fatalf("unset key: want disabled and naming the setting, got %+v", f)
	}
	cfg.TenantcorePublicKey = "set"
	if f := find(cfg); !f.Enabled {
		t.Fatalf("set key: want enabled, got %+v", f)
	}
}

// The resolver switch is a rollout control: the default must be the old
// behaviour, a typo must be refused by name, and tenantcore mode must not
// start without the link it needs.
func TestLoadDefaultsTenantResolverToLocal(t *testing.T) {
	t.Setenv("TENANT_RESOLVER", "")
	t.Setenv("TENANT_RESOLVE_RATE_PER_MINUTE", "")
	cfg := Load()
	if cfg.TenantResolver != "local" {
		t.Errorf("TenantResolver = %q, want local", cfg.TenantResolver)
	}
	if cfg.TenantResolveRatePerMinute != 600 {
		t.Errorf("TenantResolveRatePerMinute = %d, want 600", cfg.TenantResolveRatePerMinute)
	}
}

func TestLoadReadsTenantResolverSettings(t *testing.T) {
	t.Setenv("TENANT_RESOLVER", " TenantCore ")
	t.Setenv("TENANT_RESOLVE_RATE_PER_MINUTE", "50")
	cfg := Load()
	if cfg.TenantResolver != "tenantcore" || cfg.TenantResolveRatePerMinute != 50 {
		t.Errorf("got %q / %d", cfg.TenantResolver, cfg.TenantResolveRatePerMinute)
	}
}

func TestValidateRejectsAnUnknownTenantResolver(t *testing.T) {
	cfg := validConfig()
	cfg.TenantResolver = "remote"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "TENANT_RESOLVER") {
		t.Fatalf("want an error naming TENANT_RESOLVER, got %v", err)
	}
}

func TestValidateAcceptsLocalAndEmptyResolver(t *testing.T) {
	for _, v := range []string{"", "local"} {
		cfg := validConfig()
		cfg.TenantResolver = v
		if err := cfg.Validate(); err != nil {
			t.Errorf("TenantResolver=%q should be valid, got %v", v, err)
		}
	}
}

func TestValidateTenantcoreResolverNeedsURLAndKey(t *testing.T) {
	cfg := validConfig()
	cfg.TenantResolver = "tenantcore"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("tenantcore without URL and key must not validate")
	}
	for _, want := range []string{"TENANTCORE_URL", "TENANTCORE_SERVICE_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %s, got %v", want, err)
		}
	}

	cfg.TenantcoreURL = "http://localhost:1"
	cfg.TenantcoreServiceKey = "svc-test"
	cfg.TenantResolveRatePerMinute, cfg.TenantResolveBurst = 600, 120
	if err := cfg.Validate(); err != nil {
		t.Errorf("tenantcore with URL and key should validate, got %v", err)
	}
}

func TestFeaturesListsTenantcoreResolverOnlyInThatMode(t *testing.T) {
	find := func(c Config) (Feature, bool) {
		for _, f := range c.Features() {
			if f.Name == "tenant_resolver_tenantcore" {
				return f, true
			}
		}
		return Feature{}, false
	}
	local := validConfig()
	local.TenantResolver = "local"
	if _, ok := find(local); ok {
		t.Error("local mode must not add a feature entry (default /readyz stays as it was)")
	}
	tc := validConfig()
	tc.TenantResolver = "tenantcore"
	f, ok := find(tc)
	if !ok || !f.Enabled {
		t.Errorf("tenantcore mode should list the feature as enabled, got %+v ok=%v", f, ok)
	}
}

func TestTenantResolveBurstDefaultAndValidation(t *testing.T) {
	t.Setenv("TENANT_RESOLVE_BURST", "")
	if got := Load().TenantResolveBurst; got != 120 {
		t.Errorf("default TENANT_RESOLVE_BURST = %d, want 120", got)
	}

	cfg := validConfig()
	cfg.TenantResolver = "tenantcore"
	cfg.TenantcoreURL = "http://localhost:1"
	cfg.TenantcoreServiceKey = "svc-test"
	cfg.TenantResolveRatePerMinute = 600
	cfg.TenantResolveBurst = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "TENANT_RESOLVE_BURST") {
		t.Fatalf("burst 0 in tenantcore mode should be refused naming the variable, got %v", err)
	}
	cfg.TenantResolveBurst = 120
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid tenantcore config refused: %v", err)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	cases := map[string][]string{
		"":                             nil,
		"   ":                          nil,
		" , ,":                         nil,
		"10.0.0.1":                     {"10.0.0.1"},
		"10.0.0.0/8":                   {"10.0.0.0/8"},
		" 10.0.0.1 , ,2001:db8::/32 ,": {"10.0.0.1", "2001:db8::/32"},
		"10.0.0.0/8,192.168.1.5,::1":   {"10.0.0.0/8", "192.168.1.5", "::1"},
	}
	for in, want := range cases {
		got := ParseTrustedProxies(in)
		if len(got) != len(want) {
			t.Errorf("%q: got %v, want %v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q: got %v, want %v", in, got, want)
			}
		}
	}
}

func TestLoadReadsTrustedProxies(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	if c := Load(); len(c.TrustedProxies) != 0 {
		t.Errorf("unset must leave the list empty, got %v", c.TrustedProxies)
	}
	t.Setenv("TRUSTED_PROXIES", " 10.0.0.0/8 , 192.0.2.1 ")
	if c := Load(); len(c.TrustedProxies) != 2 || c.TrustedProxies[0] != "10.0.0.0/8" || c.TrustedProxies[1] != "192.0.2.1" {
		t.Errorf("got %v", c.TrustedProxies)
	}
}

func TestValidateTrustedProxies(t *testing.T) {
	for _, ok := range [][]string{nil, {"10.0.0.1"}, {"10.0.0.0/8"}, {"10.0.0.1", "2001:db8::/32", "::1"}} {
		c := validConfig()
		c.TrustedProxies = ok
		if err := c.Validate(); err != nil {
			t.Errorf("%v rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"not-an-ip", "10.0.0.0/33", "10.0.0.1:80", "999.1.1.1"} {
		c := validConfig()
		c.TrustedProxies = []string{"10.0.0.1", bad}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXIES") || !strings.Contains(err.Error(), bad) {
			t.Errorf("%q: error %v must name TRUSTED_PROXIES and the entry", bad, err)
		}
	}
}
