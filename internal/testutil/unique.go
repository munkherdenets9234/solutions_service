package testutil

import (
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
