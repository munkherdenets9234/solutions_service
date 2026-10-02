package token

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// This file is the verify side of tenantcore's Ed25519 tokens. It is a copy of
// tenantcore's pkg/token with the Maker removed: this service holds only the
// public key, so it can check a token and cannot forge one. The Tenantcore
// prefix is there because Claims and Maker in this package already mean the
// HMAC tokens this service issues itself.

// TenantcoreIssuer is the iss claim tenantcore stamps on every token.
const TenantcoreIssuer = "tenantcore"

// TenantcoreRole is the closed set of identities tenantcore issues tokens for.
type TenantcoreRole string

const (
	TenantcoreRoleSuperadmin  TenantcoreRole = "superadmin"
	TenantcoreRoleTenantAdmin TenantcoreRole = "admin"
	TenantcoreRoleTenantStaff TenantcoreRole = "staff"
)

func (r TenantcoreRole) Valid() bool {
	switch r {
	case TenantcoreRoleSuperadmin, TenantcoreRoleTenantAdmin, TenantcoreRoleTenantStaff:
		return true
	}
	return false
}

// TenantcoreClaims is the token payload; field names match Claims here.
type TenantcoreClaims struct {
	UserID   string         `json:"user_id"`
	Role     TenantcoreRole `json:"role"`
	TenantID string         `json:"tenant_id,omitempty"` // empty for platform superadmins
	jwt.RegisteredClaims
}

// Valid enforces the non-signature invariant: a superadmin token must never
// carry a tenant scope, and a tenant role must.
func (c TenantcoreClaims) Valid() error {
	if !c.Role.Valid() {
		return fmt.Errorf("token: unknown role %q", c.Role)
	}
	if c.Role == TenantcoreRoleSuperadmin && c.TenantID != "" {
		return errors.New("token: a superadmin token must not be tenant-scoped")
	}
	if c.Role != TenantcoreRoleSuperadmin && c.TenantID == "" {
		return errors.New("token: a tenant role must be tenant-scoped")
	}
	return nil
}

// TenantcoreVerifier checks tokens against tenantcore's public key.
type TenantcoreVerifier struct {
	public ed25519.PublicKey
}

// NewVerifier builds a verifier from a base64-encoded Ed25519 public key.
func NewVerifier(publicKeyB64 string) (*TenantcoreVerifier, error) {
	raw, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return nil, fmt.Errorf("token: public key is not valid base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("token: public key is %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return &TenantcoreVerifier{public: ed25519.PublicKey(raw)}, nil
}

// ErrTenantcoreToken is returned for every verification failure, with no
// detail: a caller learns nothing from why their token was refused.
var ErrTenantcoreToken = errors.New("invalid or expired token")

// Verify checks the signature, issuer, expiry and the role invariant.
func (v *TenantcoreVerifier) Verify(tokenStr string) (*TenantcoreClaims, error) {
	claims := &TenantcoreClaims{}

	t, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		// Pin the algorithm: accepting what the header asks for is the
		// classic JWT hole ("none", or HMAC keyed with the public key).
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return v.public, nil
	},
		jwt.WithIssuer(TenantcoreIssuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
	)
	if err != nil || !t.Valid {
		return nil, ErrTenantcoreToken
	}
	if err := claims.Valid(); err != nil {
		return nil, ErrTenantcoreToken
	}
	return claims, nil
}
