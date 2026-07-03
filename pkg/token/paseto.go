package token

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	ID       string `json:"jti"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	TenantID string `json:"tenant_id,omitempty"` // empty for platform-level superadmin tokens
	jwt.RegisteredClaims
}

type Maker struct {
	secret []byte
}

func NewMaker(secret string) (*Maker, error) {
	if len(secret) < 32 {
		return nil, errors.New("token secret must be at least 32 characters")
	}
	return &Maker{secret: []byte(secret)}, nil
}

func (m *Maker) CreateToken(userID, role, tenantID string, duration time.Duration) (string, *Claims, error) {
	claims := &Claims{
		ID:       uuid.NewString(),
		UserID:   userID,
		Role:     role,
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := t.SignedString(m.secret)
	return tokenStr, claims, err
}

func (m *Maker) VerifyToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	t, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil || !t.Valid {
		return nil, errors.New("invalid or expired token")
	}
	return claims, nil
}
