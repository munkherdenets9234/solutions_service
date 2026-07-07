package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"
)

var uniqueCounter int64

// Unique returns a value guaranteed unique within this test binary, used to
// build collision-free emails/slugs across parallel subtests.
func Unique() int64 {
	return time.Now().UnixNano() + atomic.AddInt64(&uniqueCounter, 1)
}

func timeNowUnixNano() int64 {
	return Unique()
}

// randomHex returns n random bytes hex-encoded. Used for test-only secrets
// (bootstrap passwords, token signing keys) so no credential-shaped literal
// has to live in the repo — each test run mints its own throwaway values for
// a database that's torn down when the test ends.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
