package domain

import (
	"testing"
)

func sp(s string) *string { return &s }

// Only errors group. Grouping an info log would put "user signed in" on a
// screen whose whole purpose is problems.
func TestFingerprintOnlyErrors(t *testing.T) {
	for _, level := range []string{"info", "debug", "warning", "verbose", "system"} {
		if got := Fingerprint(level, "something", false, nil, nil); got != "" {
			t.Errorf("%s should not be fingerprinted, got %q", level, got)
		}
	}
	for _, level := range []string{"error", "fatal"} {
		if got := Fingerprint(level, "something", false, nil, nil); got == "" {
			t.Errorf("%s should be fingerprinted", level)
		}
	}
}

// The splitting half of the problem: one bug wearing many ids.
func TestSameBugWithDifferentIdsGroups(t *testing.T) {
	groups := [][]string{
		{"User 4821 not found", "User 9134 not found", "User 7 not found"},
		{"Order ord_8812f failed", "Order ord_9921a failed"},
		{
			"Session 4f2a81b0-1111-4222-8333-444455556666 expired",
			"Session 9c073d55-7777-4888-8999-aaaabbbbcccc expired",
		},
		{"Timed out after 30000ms", "Timed out after 250ms"},
		{`Missing key "order_id"`, `Missing key "customer_id"`},
	}
	for _, group := range groups {
		first := Fingerprint("error", group[0], false, nil, nil)
		for _, msg := range group[1:] {
			if got := Fingerprint("error", msg, false, nil, nil); got != first {
				t.Errorf("these should group:\n  %q -> %q\n  %q -> %q", group[0], first, msg, got)
			}
		}
	}
}

// Normalising must not go so far that genuinely different bugs merge.
func TestDifferentBugsStaySeparate(t *testing.T) {
	a := Fingerprint("error", "Null check operator used on a null value", false, nil, nil)
	b := Fingerprint("error", "Payment declined — gateway timeout", false, nil, nil)
	c := Fingerprint("error", "User 1 not found", false, nil, nil)
	if a == b || b == c || a == c {
		t.Errorf("distinct problems collapsed: %q %q %q", a, b, c)
	}
}

// The merging half: every failed request carries the same message, so the
// message must not be what identifies it.
func TestNetworkFailuresGroupByEndpoint(t *testing.T) {
	pay := Fingerprint("error", "Network Error", true, sp("POST"), sp("https://api.ledger.dev/v2/pay"))
	cart := Fingerprint("error", "Network Error", true, sp("GET"), sp("https://api.ledger.dev/v2/cart"))

	if pay == cart {
		t.Fatal("two different endpoints failing must not be one row")
	}
	if pay != "POST /v2/pay" {
		t.Errorf("want the endpoint as the key, got %q", pay)
	}

	// Same endpoint on another host or with a different query is the same
	// problem: one row, not one per environment or per request.
	staging := Fingerprint("error", "Network Error", true, sp("POST"), sp("https://staging.ledger.dev/v2/pay?retry=2"))
	if staging != pay {
		t.Errorf("host and query should not split a group: %q vs %q", staging, pay)
	}

	// Method matters: a failing POST is not a failing GET.
	if Fingerprint("error", "Network Error", true, sp("GET"), sp("https://api.ledger.dev/v2/pay")) == pay {
		t.Error("method should be part of the key")
	}
}

// REST paths carry ids too, so one broken endpoint must not become one row per
// customer.
func TestNetworkPathIdsAreBlanked(t *testing.T) {
	a := Fingerprint("error", "Network Error", true, sp("GET"), sp("https://api.dev/users/4821/orders"))
	b := Fingerprint("error", "Network Error", true, sp("GET"), sp("https://api.dev/users/9134/orders"))
	if a != b {
		t.Errorf("path ids should be blanked: %q vs %q", a, b)
	}
	if a != "GET /users/{id}/orders" {
		t.Errorf("got %q", a)
	}

	// A word segment is a route, not an id, and must survive.
	c := Fingerprint("error", "Network Error", true, sp("GET"), sp("https://api.dev/users/me/orders"))
	if c == a {
		t.Error("/users/me should not collapse into /users/{id}")
	}
}

// A network error with nothing identifying it still has to group somehow,
// rather than producing an empty key that groups with every other blank.
func TestNetworkWithoutRequestFallsBackToMessage(t *testing.T) {
	got := Fingerprint("error", "Network Error", true, nil, nil)
	if got != "Network Error" {
		t.Errorf("want the message as a last resort, got %q", got)
	}
}

// An error whose message carries its own stack trace would never match another
// occurrence if the whole thing were the key.
func TestOnlyTheFirstLineIsUsed(t *testing.T) {
	a := Fingerprint("error", "Bad state: no element\n#0 first (file.dart:1:2)", false, nil, nil)
	b := Fingerprint("error", "Bad state: no element\n#0 other (file.dart:9:9)", false, nil, nil)
	if a != b {
		t.Errorf("differing traces should not split the group: %q vs %q", a, b)
	}
}

// Some errors are whole JSON documents. One of those must not become the key.
func TestFingerprintIsBounded(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "some words that go on and on "
	}
	if got := Fingerprint("error", long, false, nil, nil); len(got) > fingerprintMaxLen {
		t.Errorf("fingerprint is %d chars, want at most %d", len(got), fingerprintMaxLen)
	}
}
