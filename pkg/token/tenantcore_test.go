package token

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// tcMint signs a tenantcore-shaped token with a locally generated key, so
// these tests do not depend on tenantcore's own code.
func tcMint(t *testing.T, priv ed25519.PrivateKey, role, tenantID string, ttl time.Duration) string {
	t.Helper()
	now := time.Now()
	claims := TenantcoreClaims{
		UserID:   "u1",
		Role:     TenantcoreRole(role),
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TenantcoreIssuer,
			Subject:   "u1",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func tcKeys(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, base64.StdEncoding.EncodeToString(pub)
}

func tcVerifier(t *testing.T, pubB64 string) *TenantcoreVerifier {
	t.Helper()
	v, err := NewVerifier(pubB64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTenantcoreVerify_ValidSuperadmin(t *testing.T) {
	priv, pub := tcKeys(t)
	claims, err := tcVerifier(t, pub).Verify(tcMint(t, priv, "superadmin", "", time.Hour))
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if claims.Role != TenantcoreRoleSuperadmin || claims.UserID != "u1" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestTenantcoreVerify_RejectsHMACToken(t *testing.T) {
	_, pub := tcKeys(t)
	claims := TenantcoreClaims{
		UserID: "u1", Role: TenantcoreRoleSuperadmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TenantcoreIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tcVerifier(t, pub).Verify(s); err == nil {
		t.Fatal("an HS256 token must be rejected")
	}
}

func TestTenantcoreVerify_RejectsExpired(t *testing.T) {
	priv, pub := tcKeys(t)
	if _, err := tcVerifier(t, pub).Verify(tcMint(t, priv, "superadmin", "", -time.Hour)); err == nil {
		t.Fatal("an expired token must be rejected")
	}
}

func TestTenantcoreVerify_RejectsTamperedSignature(t *testing.T) {
	priv, pub := tcKeys(t)
	s := tcMint(t, priv, "superadmin", "", time.Hour)
	// Flip the first signature character to a different one.
	i := len(s) - len(s[len(s)-86:])
	b := []byte(s)
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	if _, err := tcVerifier(t, pub).Verify(string(b)); err == nil {
		t.Fatal("a tampered signature must be rejected")
	}
}

func TestTenantcoreVerify_RejectsWrongKey(t *testing.T) {
	priv, _ := tcKeys(t)
	_, otherPub := tcKeys(t)
	if _, err := tcVerifier(t, otherPub).Verify(tcMint(t, priv, "superadmin", "", time.Hour)); err == nil {
		t.Fatal("a token signed by another key must be rejected")
	}
}

func TestTenantcoreVerify_RejectsSuperadminWithTenantID(t *testing.T) {
	priv, pub := tcKeys(t)
	if _, err := tcVerifier(t, pub).Verify(tcMint(t, priv, "superadmin", "t1", time.Hour)); err == nil {
		t.Fatal("a tenant-scoped superadmin token must be rejected")
	}
}

func TestNewVerifier_RejectsBadKey(t *testing.T) {
	if _, err := NewVerifier("not base64!!"); err == nil {
		t.Fatal("bad base64 must fail")
	}
	if _, err := NewVerifier(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("wrong length must fail")
	}
}

// tcMintAt signs a token with explicit nbf and exp, for clock-skew cases.
func tcMintAt(t *testing.T, priv ed25519.PrivateKey, nbf, exp time.Time) string {
	t.Helper()
	claims := TenantcoreClaims{
		UserID: "u1",
		Role:   TenantcoreRoleSuperadmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TenantcoreIssuer,
			NotBefore: jwt.NewNumericDate(nbf),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// tenantcore and this service run on different clocks; a token minted a moment
// "in the future" by a fast clock must not bounce a valid operator, while a
// token that is genuinely stale still must.
func TestTenantcoreVerify_ToleratesSmallClockSkew(t *testing.T) {
	priv, pub := tcKeys(t)
	v := tcVerifier(t, pub)
	now := time.Now()

	if _, err := v.Verify(tcMintAt(t, priv, now.Add(10*time.Second), now.Add(time.Hour))); err != nil {
		t.Fatalf("a token whose nbf is 10s ahead must be accepted: %v", err)
	}
	if _, err := v.Verify(tcMintAt(t, priv, now.Add(-time.Hour), now.Add(-60*time.Second))); err == nil {
		t.Fatal("a token expired by 60s must still be rejected")
	}
}
