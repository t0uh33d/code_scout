package middleware

import (
	"net/http"
	"strings"
)

// SecurityHeaders sets the response headers a browser needs to be told, since
// it assumes the permissive answer for every one of them.
//
// The dashboard is an authenticated HTML application: a session cookie, forms
// that rotate secrets and delete projects, and a live view of somebody's
// device. None of the headers below were set at all, so it could be framed by
// any site, its assets were sniffable, and full URLs leaked to anything it
// linked to.
//
// Applied on the root router so it covers the dashboard, the SDK API and
// /api/mcp alike. The API routes do not need most of it and are not harmed by
// any of it, and one place that cannot be forgotten beats three that can.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Nothing here is ever meant to be framed. The live session screen and
		// the settings forms are exactly what a clickjacking overlay would want
		// to sit a transparent button on top of.
		h.Set("X-Frame-Options", "DENY")

		// Serve every asset as the type it was declared, so an uploaded or
		// echoed body cannot be re-read as script.
		h.Set("X-Content-Type-Options", "nosniff")

		// A log viewer URL carries the search query, which is the operator's
		// own data. Send the origin to other sites and nothing at all when
		// leaving HTTPS.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Features this dashboard has no use for. Denied rather than left to
		// whatever the browser's default becomes.
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")

		// HSTS only over TLS. Sent on a plain-HTTP dev instance it would pin
		// the browser to a scheme that instance does not serve, and the operator
		// would have to clear it by hand to get back in.
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// The CSP is deliberately not on the API routes: it describes what an
		// HTML document may load, and a JSON body has no such thing.
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Content-Security-Policy", dashboardCSP)
		}

		next.ServeHTTP(w, r)
	})
}

// dashboardCSP is what the dashboard actually needs, and no more.
//
//   - 'unsafe-inline' for script is required by the inline scripts and onclick
//     attributes the dashboard's screens carry. Removing it means moving those
//     into files with nonces, which is worth doing and is not a security fix to
//     be smuggled into one.
//   - 'unsafe-eval' is deliberately absent, and that constrains what the
//     templates may use. htmx compiles hx-on, hx-vals="js:…", hx-headers="js:…"
//     and hx-trigger filter expressions with new Function, which is eval. The
//     first CSP shipped with two hx-on attributes still in the templates, and
//     both sheets silently stopped opening: the request went out and returned
//     200, and the handler that opens the sheet never ran. Nothing appears in
//     the server log, only a console error in the browser.
//     TestTheViewsDoNotUseAnHTMXFeatureThatNeedsEval keeps them out.
//   - The three external origins are the ones view/ actually references:
//     cdn.jsdelivr.net for htmx and its SSE extension, fonts.googleapis.com for
//     the stylesheet and fonts.gstatic.com for the font files. They are listed
//     rather than silently blocked, because a CSP that breaks the dashboard
//     gets turned off rather than corrected. TestTheCSPCoversEveryOriginTheViewsUse
//     derives them from the templates, so adding a fourth fails there first.
//   - connect-src keeps 'self' plus ws/wss, which is the SSE and WebSocket the
//     live screens open back to this same origin.
//   - frame-ancestors repeats X-Frame-Options for browsers that prefer it, and
//     is the one modern browsers actually read.
const dashboardCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"img-src 'self' data:; " +
	"font-src 'self' data: https://fonts.gstatic.com; " +
	"connect-src 'self' ws: wss:; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"
