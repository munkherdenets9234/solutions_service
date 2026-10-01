package password

import (
	"testing"
	"time"
)

// DummyCompare exists to make the "no such account" path cost what a real check
// costs. If it ever becomes free, unknown and known accounts differ by tens of
// milliseconds again.
func TestDummyCompareSpendsRealWork(t *testing.T) {
	real, err := Hash("some-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	start := time.Now()
	Verify(real, "some-password")
	verifyCost := time.Since(start)

	start = time.Now()
	DummyCompare()
	dummyCost := time.Since(start)

	// Same order of magnitude: not free, not wildly slower.
	if dummyCost < verifyCost/4 {
		t.Fatalf("DummyCompare took %v, a real check took %v: it is not spending comparable time", dummyCost, verifyCost)
	}
}
