package unsubscribe

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// testKey builds a key at run time so no secret-looking literal is committed.
func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

var now = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func TestSignVerifyRoundTrip(t *testing.T) {
	key := testKey(t)
	tid, uid := primitive.NewObjectID(), primitive.NewObjectID()
	tok := Sign(key, tid, uid, now.Add(90*24*time.Hour))

	gotT, gotU, err := Verify(key, tok, now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if gotT != tid || gotU != uid {
		t.Fatalf("ids differ: got %s/%s want %s/%s", gotT, gotU, tid, uid)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	key := testKey(t)
	tok := Sign(key, primitive.NewObjectID(), primitive.NewObjectID(), now.Add(-time.Second))
	if _, _, err := Verify(key, tok, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
	// Exactly at expiry is already expired.
	tok = Sign(key, primitive.NewObjectID(), primitive.NewObjectID(), now)
	if _, _, err := Verify(key, tok, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("at expiry: want ErrInvalidToken, got %v", err)
	}
}

func flip(t *testing.T, seg string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil || len(raw) == 0 {
		t.Fatalf("bad segment: %v", err)
	}
	raw[len(raw)-1] ^= 0x01
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestVerifyRejectsTamperedPayloadAndSignature(t *testing.T) {
	key := testKey(t)
	tok := Sign(key, primitive.NewObjectID(), primitive.NewObjectID(), now.Add(time.Hour))
	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		t.Fatalf("token should be payload.signature, got %d parts", len(parts))
	}
	for name, bad := range map[string]string{
		"payload":   flip(t, parts[0]) + "." + parts[1],
		"signature": parts[0] + "." + flip(t, parts[1]),
	} {
		if _, _, err := Verify(key, bad, now); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%s tampered: want ErrInvalidToken, got %v", name, err)
		}
	}
}

func TestVerifyUsesOneErrorForAllFailures(t *testing.T) {
	key := testKey(t)
	good := Sign(key, primitive.NewObjectID(), primitive.NewObjectID(), now.Add(time.Hour))
	expired := Sign(key, primitive.NewObjectID(), primitive.NewObjectID(), now.Add(-time.Hour))
	parts := strings.Split(good, ".")
	cases := map[string]string{
		"empty":         "",
		"no separator":  "abc",
		"three parts":   good + ".AAAA",
		"bad base64":    "!!!." + parts[1],
		"bad sig b64":   parts[0] + ".!!!",
		"short payload": base64.RawURLEncoding.EncodeToString([]byte("x")) + "." + parts[1],
		"short sig":     parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte("x")),
		"expired":       expired,
		"bad signature": parts[0] + "." + flip(t, parts[1]),
	}
	for name, tok := range cases {
		_, _, err := Verify(key, tok, now)
		if err != ErrInvalidToken {
			t.Errorf("%s: want exactly ErrInvalidToken, got %v", name, err)
			continue
		}
		if tok != "" && strings.Contains(err.Error(), tok) {
			t.Errorf("%s: error echoes the token", name)
		}
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	tok := Sign(testKey(t), primitive.NewObjectID(), primitive.NewObjectID(), now.Add(time.Hour))
	if _, _, err := Verify(testKey(t), tok, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
}
