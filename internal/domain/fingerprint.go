package domain

import (
	"regexp"
	"strings"
)

// Fingerprinting is what turns a wall of individual errors into a short list of
// distinct problems.
//
// Grouping on the raw message fails in both directions at once. It SPLITS one
// problem into many, because "User 4821 not found" and "User 9134 not found"
// are one bug wearing two ids. And it MERGES many problems into one, because
// every failed request the SDK reports carries the literal message "Network
// Error" — so a payment gateway timing out and an analytics ping being blocked
// would land in the same row, hiding the only thing worth knowing, which is
// what failed.
//
// So: ordinary errors fingerprint on the message with its varying parts blanked, and
// network failures ignore the message entirely and fingerprint on the request.

var (
	// Ordered most specific first: a UUID would otherwise be eaten piecemeal by
	// the hex and number rules and stop matching another UUID.
	uuidPattern  = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	hexPattern   = regexp.MustCompile(`(?i)\b(?:0x)?[0-9a-f]{8,}\b`)
	quotedSingle = regexp.MustCompile(`'[^']*'`)
	quotedDouble = regexp.MustCompile(`"[^"]*"`)
	// A token mixing letters and digits is an identifier, not prose: ord_8812f,
	// user4821, req_9f3a. Blanking only its digits would leave the stray
	// letters behind and split the group again, which is what "ord_{n}f" vs
	// "ord_{n}a" did.
	mixedIDPattern = regexp.MustCompile(`\b(?:[A-Za-z_-]*\d[A-Za-z0-9_-]*[A-Za-z_-]+[A-Za-z0-9_-]*|[A-Za-z_-]+[A-Za-z0-9_-]*\d[A-Za-z0-9_-]*)\b`)
	// Any remaining run of digits, including decimals.
	numberPattern = regexp.MustCompile(`\d+(?:\.\d+)?`)
	// Collapses the whitespace that blanking can leave behind.
	spacePattern = regexp.MustCompile(`\s+`)
)

// fingerprintMaxLen keeps one runaway message from becoming a column-wide key.
// Errors are sometimes whole JSON documents.
const fingerprintMaxLen = 300

// Fingerprint returns the grouping key for a log, or "" for anything that is
// not an error — only errors and fatals are grouped.
func Fingerprint(level string, message string, isNetwork bool, method, url *string) string {
	if level != "error" && level != "fatal" {
		return ""
	}

	// A failed request is identified by what it called, not by the message,
	// which is the same string every time.
	if isNetwork {
		if key := networkFingerprint(method, url); key != "" {
			return key
		}
	}

	return normaliseMessage(message)
}

// networkFingerprint is method plus path: "POST /v2/checkout/session". The host
// is dropped so the same endpoint groups across environments, and the query
// string is dropped because it is nearly always per-request.
// Callers pass the promoted columns; ingest fills those from the metadata just
// before calling here, so there is nothing else to consult.
func networkFingerprint(method, rawURL *string) string {
	if method == nil || rawURL == nil || *method == "" || *rawURL == "" {
		return ""
	}
	return strings.ToUpper(*method) + " " + normalisePath(pathOf(*rawURL))
}

// pathOf strips scheme, host and query without url.Parse, which would reject
// the malformed URLs that show up in real error payloads.
func pathOf(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.Index(s, "/"); j >= 0 {
			s = s[j:]
		} else {
			s = "/"
		}
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "/"
	}
	return s
}

// normalisePath blanks the ids embedded in REST paths, so /users/4821/orders
// and /users/9134/orders are one endpoint rather than one row per customer.
func normalisePath(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		switch {
		case seg == "":
			continue
		case uuidPattern.MatchString(seg):
			segments[i] = "{id}"
		case numberPattern.MatchString(seg) && !strings.ContainsAny(seg, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"):
			segments[i] = "{id}"
		}
	}
	return strings.Join(segments, "/")
}

// normaliseMessage blanks the parts of a message that differ between
// occurrences of the same bug.
func normaliseMessage(message string) string {
	s := strings.TrimSpace(message)
	if s == "" {
		return ""
	}

	// Only the first line: an error whose message carries its own stack trace
	// would otherwise never match another occurrence.
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}

	s = uuidPattern.ReplaceAllString(s, "{uuid}")
	s = quotedDouble.ReplaceAllString(s, `"{str}"`)
	s = quotedSingle.ReplaceAllString(s, `'{str}'`)
	s = hexPattern.ReplaceAllString(s, "{hex}")
	s = mixedIDPattern.ReplaceAllString(s, "{id}")
	s = numberPattern.ReplaceAllString(s, "{n}")
	s = spacePattern.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)

	if len(s) > fingerprintMaxLen {
		s = s[:fingerprintMaxLen]
	}
	return s
}
