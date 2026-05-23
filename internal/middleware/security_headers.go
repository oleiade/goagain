package middleware

import "net/http"

// SecurityHeaders returns a middleware that sets the small set of always-safe
// response headers (nosniff, X-Frame-Options, Referrer-Policy). It is
// intentionally not setting Content-Security-Policy: the /docs endpoint loads
// a version-pinned Scalar bundle from a CDN and a strict default-src would
// break it. For a JSON-only API there is no XSS sink to protect anyway;
// nosniff is the load-bearing header here.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			next.ServeHTTP(w, r)
		})
	}
}
