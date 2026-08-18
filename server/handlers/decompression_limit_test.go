package handlers

import "testing"

// The decompression bound is what stands between a valid project credential and
// asking the server for as much heap as it will give. It has to hold whatever
// the upload cap is set to, because the cap is tuned for upload size and this
// is tuned for memory.
func TestTheDecompressionLimitIsAbsolute(t *testing.T) {
	const mb = 1 << 20

	for _, tc := range []struct {
		name string
		cap  int64
		want int64
	}{
		// Small caps: the ratio is the tighter of the two and should win, so a
		// modest instance is not handed a 64 MB allowance it never asked for.
		{"1MB cap", 1 * mb, 5 * mb},
		{"8MB cap", 8 * mb, 40 * mb},

		// The default and above: the ceiling binds. 50 MB x 5 is 250 MB of
		// inflated tar, accumulated whole before a row is written.
		{"default 50MB cap", 50 * mb, maxDecompressedBytes},
		{"generous 500MB cap", 500 * mb, maxDecompressedBytes},

		// Raising the upload cap must not raise the memory ceiling with it.
		{"absurd cap", 100000 * mb, maxDecompressedBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decompressionLimit(tc.cap); got != tc.want {
				t.Errorf("decompressionLimit(%d) = %d, want %d", tc.cap, got, tc.want)
			}
		})
	}
}

// A gzip bomb's whole trick is that the compressed side is tiny, so the upload
// cap never fires. Stated as a test because it is the reason the ceiling exists
// and it is not obvious from the constant alone.
func TestASmallUploadCannotAskForUnboundedMemory(t *testing.T) {
	const cap = 50 << 20 // the default

	limit := decompressionLimit(cap)
	if limit > maxDecompressedBytes {
		t.Fatalf("limit %d exceeds the ceiling %d", limit, maxDecompressedBytes)
	}

	// 1000:1 is ordinary for a stream of zeros, so this is the compressed size
	// an attacker needs to reach the bound. It must stay far below the upload
	// cap, or the cap would have caught it and the ceiling would be redundant.
	needed := limit / 1000
	if needed >= cap {
		t.Fatalf("a bomb reaching the decompression bound would need %d "+
			"compressed bytes, which the %d upload cap already refuses; "+
			"the ceiling is doing nothing", needed, cap)
	}
}
