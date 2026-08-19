package services

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// A login for an address with no account has to cost what one with an account
// costs.
//
// bcrypt only ran once a row came back, so the two answers were about seventy
// milliseconds apart. That gap is legible over a network and is the whole of
// account enumeration on an instance where nobody can sign up: an attacker
// learns which addresses are real before trying a single password.
//
// Asserted as a floor rather than a comparison between the two paths. A
// like-for-like timing assertion is the flaky kind on shared CI, whereas the
// gap being closed at all is the difference between microseconds and tens of
// milliseconds, and a floor well under bcrypt's real cost cannot flap.
func TestAnUnknownAccountStillCostsAPasswordCheck(t *testing.T) {
	if timingDummyHash == nil {
		t.Fatal("the dummy hash was never built, so the unknown-account path " +
			"returns immediately and the timing gap is wide open")
	}

	start := time.Now()
	equaliseLoginTiming("whatever they typed")
	elapsed := time.Since(start)

	// bcrypt at DefaultCost is tens of milliseconds on any machine this runs
	// on. Without the equaliser this path is sub-millisecond.
	const floor = 5 * time.Millisecond
	if elapsed < floor {
		t.Errorf("the unknown-account path took %v, under the %v floor: no "+
			"password check was performed, so response time still says whether "+
			"an address is registered", elapsed, floor)
	}
}

// The dummy has to cost the same as the real thing, which means the same cost
// parameter. A cheaper one would leave a smaller gap rather than no gap.
func TestTheDummyHashMatchesTheRealCost(t *testing.T) {
	cost, err := bcrypt.Cost(timingDummyHash)
	if err != nil {
		t.Fatalf("the dummy hash is not a bcrypt hash: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Errorf("dummy hash cost is %d, want %d — the same cost registration "+
			"and password reset use, or the paths still differ", cost, bcrypt.DefaultCost)
	}
}

// It must never be possible to authenticate against the equaliser itself.
func TestTheDummyHashMatchesNothing(t *testing.T) {
	for _, guess := range []string{"", "password", "admin", "hunter2"} {
		if bcrypt.CompareHashAndPassword(timingDummyHash, []byte(guess)) == nil {
			t.Errorf("the dummy hash accepted %q", guess)
		}
	}
}
