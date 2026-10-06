// Package unsubscribe signs and verifies the one-click unsubscribe tokens that
// go into every request email.
//
// A token is base64url(payload) "." base64url(mac). The payload is the tenant
// id (12 bytes), the user id (12 bytes) and the expiry as Unix seconds (8
// bytes, big endian). The mac is HMAC-SHA256 over the payload.
package unsubscribe

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ErrInvalidToken is the only error Verify returns. Malformed, expired and
// forged tokens are deliberately indistinguishable to the caller.
var ErrInvalidToken = errors.New("invalid unsubscribe token")

const payloadLen = 12 + 12 + 8

func mac(key, payload []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(payload)
	return h.Sum(nil)
}

// Sign returns a token for the tenant and user that is valid until expires.
func Sign(key []byte, tenantID, userID primitive.ObjectID, expires time.Time) string {
	payload := make([]byte, payloadLen)
	copy(payload[0:12], tenantID[:])
	copy(payload[12:24], userID[:])
	binary.BigEndian.PutUint64(payload[24:], uint64(expires.Unix()))
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(key, payload))
}

// Verify checks the signature and expiry and returns the ids. Every failure
// returns ErrInvalidToken.
func Verify(key []byte, token string, now time.Time) (tenantID, userID primitive.ObjectID, err error) {
	fail := func() (primitive.ObjectID, primitive.ObjectID, error) {
		return primitive.NilObjectID, primitive.NilObjectID, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return fail()
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(parts[0])
	if err != nil || len(payload) != payloadLen {
		return fail()
	}
	sig, err := enc.DecodeString(parts[1])
	if err != nil || len(sig) != sha256.Size {
		return fail()
	}
	if subtle.ConstantTimeCompare(sig, mac(key, payload)) != 1 {
		return fail()
	}
	exp := int64(binary.BigEndian.Uint64(payload[24:]))
	if exp <= now.Unix() {
		return fail()
	}
	copy(tenantID[:], payload[0:12])
	copy(userID[:], payload[12:24])
	return tenantID, userID, nil
}
