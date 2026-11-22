package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAllowedOrigins(t *testing.T) {
	tests := []struct {
		name        string
		frontendURL string
		appEnv      string
		want        []string
	}{
		{"production", "https://watchlistnotify.com", "production", []string{"https://watchlistnotify.com"}},
		{"production, any case", "https://watchlistnotify.com", " Production ", []string{"https://watchlistnotify.com"}},
		{"a path and trailing slash are dropped", "https://watchlistnotify.com/app/", "production", []string{"https://watchlistnotify.com"}},
		{"a port is kept", "https://staging.example.com:8443", "production", []string{"https://staging.example.com:8443"}},
		{"development adds the local dev server", "https://watchlistnotify.com", "local", []string{"https://watchlistnotify.com", "http://localhost:3000", "http://127.0.0.1:3000"}},
		{"local frontend is not listed twice", "http://localhost:3000", "", []string{"http://localhost:3000", "http://127.0.0.1:3000"}},
		{"nothing configured outside production", "", "", []string{"http://localhost:3000", "http://127.0.0.1:3000"}},
		{"nothing configured in production", "", "production", nil},
		{"a wildcard is not an origin", "*", "production", nil},
		{"a bare host is not an origin", "watchlistnotify.com", "production", nil},
		{"other schemes are refused", "javascript://watchlistnotify.com", "production", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allowedOrigins(tt.frontendURL, tt.appEnv); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("allowedOrigins(%q, %q) = %v, want %v", tt.frontendURL, tt.appEnv, got, tt.want)
			}
		})
	}
}

func TestCORSAllowsOnlyTheFrontend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(newCORS(allowedOrigins("https://watchlistnotify.com", "production")))
	r.GET("/api/watchlist", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := func(method, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/watchlist", nil)
		req.Header.Set("Origin", origin)
		if method == http.MethodOptions {
			req.Header.Set("Access-Control-Request-Method", http.MethodGet)
			req.Header.Set("Access-Control-Request-Headers", "authorization")
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	allowed := request(http.MethodGet, "https://watchlistnotify.com")
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "https://watchlistnotify.com" {
		t.Errorf("Allow-Origin for the frontend = %q", got)
	}
	if got := allowed.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q", got)
	}
	if got := allowed.Header().Get("Access-Control-Expose-Headers"); got != "Server-Timing" {
		t.Errorf("Expose-Headers = %q, want Server-Timing", got)
	}

	preflight := request(http.MethodOptions, "https://watchlistnotify.com")
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Errorf("preflight from the frontend: status %d, headers %v", preflight.Code, preflight.Header())
	}

	for _, origin := range []string{"https://evil.example", "http://localhost:3000", "https://watchlistnotify.com.evil.example"} {
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			rec := request(method, origin)
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("%s from %s got Allow-Origin %q, want none", method, origin, got)
			}
		}
	}
}
