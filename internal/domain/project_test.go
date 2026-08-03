package domain

import "testing"

// The range is inclusive at both ends. 0 is a real setting — a project that has
// been switched off — and 100 is the default.
func TestValidSampleRate(t *testing.T) {
	for _, n := range []int{0, 1, 50, 99, 100} {
		if !ValidSampleRate(n) {
			t.Errorf("%d should be a valid rate", n)
		}
	}
	for _, n := range []int{-1, 101, 1000} {
		if ValidSampleRate(n) {
			t.Errorf("%d should be rejected, not clamped: a typo that silently became 100 would leave the firehose on", n)
		}
	}
}

// The SDK works in 0.0 to 1.0 and the dashboard in whole percent, so this is
// the one place the two meet.
func TestSampleFraction(t *testing.T) {
	cases := map[int]float64{0: 0, 1: 0.01, 25: 0.25, 100: 1}
	for rate, want := range cases {
		p := &Project{SessionSampleRate: rate}
		if got := p.SampleFraction(); got != want {
			t.Errorf("%d%% should be %v, got %v", rate, want, got)
		}
	}
}
