package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/neilsmahajan/watchlist-notify/internal/authtest"
)

const (
	testResource = "https://api.example.test/mcp"
	testMetadata = "https://api.example.test/.well-known/oauth-protected-resource/mcp"
	testRESTAud  = "https://api.example.test"
	testEmail    = "viewer@example.com"
	bothScopes   = ScopeWatchlistRead + " " + ScopeWatchlistWrite
)

func newTestAuth(t *testing.T, iss *authtest.Issuer) *MCPAuth {
	t.Helper()
	a, err := NewMCPAuth(MCPAuthConfig{Issuer: iss.URL(), ResourceURL: testResource, MetadataURL: testMetadata})
	if err != nil {
		t.Fatalf("NewMCPAuth: %v", err)
	}
	return a
}

func TestMCPVerifyAcceptsATenantTokenForThisResource(t *testing.T) {
	iss := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)

	info, err := a.Verify(context.Background(), iss.Token(testResource, testEmail, bothScopes), nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !HasScope(info, ScopeWatchlistRead) || !HasScope(info, ScopeWatchlistWrite) || HasScope(info, "admin") {
		t.Errorf("scopes = %v", info.Scopes)
	}
	if info.UserID != "auth0|"+testEmail {
		t.Errorf("UserID = %q", info.UserID)
	}
	if until := time.Until(info.Expiration); until < 4*time.Minute || until > 5*time.Minute {
		t.Errorf("Expiration is %v away, want about five minutes", until)
	}
	id, ok := MCPIdentityFrom(info)
	want := MCPIdentity{Subject: "auth0|" + testEmail, Email: testEmail, Name: "Test User", Picture: "https://example.test/picture.png"}
	if !ok || id != want {
		t.Errorf("identity = %+v, %v; want %+v", id, ok, want)
	}
}

func TestMCPVerifyAcceptsASingleStringAudience(t *testing.T) {
	iss := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)
	claims := iss.Claims(testResource, testEmail, bothScopes)
	claims["aud"] = testResource

	if _, err := a.Verify(context.Background(), iss.Sign(claims), nil); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestMCPVerifyRefusesEverythingElse(t *testing.T) {
	iss := authtest.NewIssuer(t)
	other := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)

	mutated := func(edit func(authtest.Claims)) string {
		claims := iss.Claims(testResource, testEmail, bothScopes)
		edit(claims)
		return iss.Sign(claims)
	}
	tests := []struct {
		name  string
		token string
	}{
		{"not a JWT", "not-a-token"},
		{"empty", ""},
		{"issued for the REST API", iss.Token(testRESTAud, testEmail, bothScopes)},
		{"audience is a prefix of the resource", iss.Token("https://api.example.test", testEmail, bothScopes)},
		{"audience differs by a trailing slash", iss.Token(testResource+"/", testEmail, bothScopes)},
		{"only the userinfo audience", mutated(func(c authtest.Claims) { c["aud"] = []string{iss.URL().String() + "userinfo"} })},
		{"no audience", mutated(func(c authtest.Claims) { delete(c, "aud") })},
		{"expired", mutated(func(c authtest.Claims) { c["exp"] = time.Now().Add(-10 * time.Minute).Unix() })},
		{"no expiry", mutated(func(c authtest.Claims) { delete(c, "exp") })},
		{"not valid yet", mutated(func(c authtest.Claims) { c["nbf"] = time.Now().Add(10 * time.Minute).Unix() })},
		{"another issuer in the claim", mutated(func(c authtest.Claims) { c["iss"] = "https://evil.example/" })},
		{"issuer without the trailing slash", mutated(func(c authtest.Claims) { c["iss"] = strings.TrimSuffix(iss.URL().String(), "/") })},
		{"signed by another issuer", other.Token(testResource, testEmail, bothScopes)},
		{"forged with another key", iss.SignWithOtherKey(iss.Claims(testResource, testEmail, bothScopes))},
		{"HS256 with the public key as the secret", iss.SignHS256(iss.Claims(testResource, testEmail, bothScopes))},
		{"email not verified", mutated(func(c authtest.Claims) { c[authtest.ClaimEmailVerified] = false })},
		{"email_verified missing", mutated(func(c authtest.Claims) { delete(c, authtest.ClaimEmailVerified) })},
		{"email_verified is the string true", mutated(func(c authtest.Claims) { c[authtest.ClaimEmailVerified] = "true" })},
		{"email_verified is 1", mutated(func(c authtest.Claims) { c[authtest.ClaimEmailVerified] = 1 })},
		{"email missing", mutated(func(c authtest.Claims) { delete(c, authtest.ClaimEmail) })},
		{"email blank", mutated(func(c authtest.Claims) { c[authtest.ClaimEmail] = "  " })},
		{"only the standard email claims", mutated(func(c authtest.Claims) {
			delete(c, authtest.ClaimEmail)
			delete(c, authtest.ClaimEmailVerified)
			c["email"], c["email_verified"] = testEmail, true
		})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := a.Verify(context.Background(), tt.token, nil)
			if !errors.Is(err, auth.ErrInvalidToken) {
				t.Fatalf("Verify = %v, %v; want auth.ErrInvalidToken", info, err)
			}
			if tt.token != "" && strings.Contains(err.Error(), tt.token) {
				t.Errorf("the error repeats the token: %v", err)
			}
		})
	}
}

func TestMCPVerifyReportsUnavailableKeysAsSuch(t *testing.T) {
	iss := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)
	iss.Down.Store(true)

	_, err := a.Verify(context.Background(), iss.Token(testResource, testEmail, bothScopes), nil)
	if !errors.Is(err, ErrKeysUnavailable) || errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("Verify = %v, want ErrKeysUnavailable and not ErrInvalidToken", err)
	}

	// The failure is not cached: the next request fetches the keys again.
	iss.Down.Store(false)
	if _, err := a.Verify(context.Background(), iss.Token(testResource, testEmail, bothScopes), nil); err != nil {
		t.Fatalf("Verify after the issuer came back: %v", err)
	}
}

func TestMCPVerifyCachesTheSigningKeys(t *testing.T) {
	iss := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)
	for range 5 {
		if _, err := a.Verify(context.Background(), iss.Token(testResource, testEmail, bothScopes), nil); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}
	if got := iss.KeyFetches.Load(); got != 1 {
		t.Errorf("the key set was fetched %d times for 5 tokens, want 1", got)
	}
}

func TestNewMCPAuthNeedsItsConfiguration(t *testing.T) {
	iss := authtest.NewIssuer(t)
	for name, cfg := range map[string]MCPAuthConfig{
		"no issuer":   {ResourceURL: testResource, MetadataURL: testMetadata},
		"no resource": {Issuer: iss.URL(), MetadataURL: testMetadata},
		"no metadata": {Issuer: iss.URL(), ResourceURL: testResource},
	} {
		if _, err := NewMCPAuth(cfg); err == nil {
			t.Errorf("%s: NewMCPAuth succeeded", name)
		}
	}
}

// guarded serves a request through RequireToken and reports whether the
// handler behind it ran and what the SDK's context held.
func guarded(t *testing.T, a *MCPAuth, edit func(*http.Request)) (rec *httptest.ResponseRecorder, reached *auth.TokenInfo) {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = auth.TokenInfoFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	if edit != nil {
		edit(req)
	}
	rec = httptest.NewRecorder()
	a.RequireToken(next).ServeHTTP(rec, req)
	return rec, reached
}

func bearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func TestRequireTokenChallengesARequestWithoutAToken(t *testing.T) {
	iss := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)

	const want = `Bearer resource_metadata="https://api.example.test/.well-known/oauth-protected-resource/mcp", scope="watchlist:read watchlist:write"`
	for name, edit := range map[string]func(*http.Request){
		"no header":      nil,
		"another scheme": func(r *http.Request) { r.Header.Set("Authorization", "Basic dXNlcjpwYXNz") },
		"no token":       func(r *http.Request) { r.Header.Set("Authorization", "Bearer") },
		// A valid token in a cookie is not a credential here.
		"token in a cookie": func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "auth_token", Value: iss.Token(testResource, testEmail, bothScopes)})
		},
		"token in the query": func(r *http.Request) {
			r.URL.RawQuery = "access_token=" + iss.Token(testResource, testEmail, bothScopes)
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec, reached := guarded(t, a, edit)
			if rec.Code != http.StatusUnauthorized || reached != nil {
				t.Fatalf("status = %d, handler reached = %v; want 401 and not reached", rec.Code, reached != nil)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != want {
				t.Errorf("WWW-Authenticate =\n%s\nwant\n%s", got, want)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q", got)
			}
		})
	}
}

func TestRequireTokenRefusesAnInvalidToken(t *testing.T) {
	iss := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)
	token := iss.Token(testRESTAud, testEmail, bothScopes) // wrong audience

	rec, reached := guarded(t, a, bearer(token))
	if rec.Code != http.StatusUnauthorized || reached != nil {
		t.Fatalf("status = %d, handler reached = %v; want 401 and not reached", rec.Code, reached != nil)
	}
	const want = `Bearer error="invalid_token", error_description="The access token is invalid or has expired.", ` +
		`resource_metadata="https://api.example.test/.well-known/oauth-protected-resource/mcp", scope="watchlist:read watchlist:write"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate =\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Errorf("the response repeats the token: %s", rec.Body)
	}
}

func TestRequireTokenNeedsTheReadScope(t *testing.T) {
	iss := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)

	for _, scope := range []string{"", "openid profile", ScopeWatchlistWrite, "watchlist:read:extra", "xwatchlist:read"} {
		rec, reached := guarded(t, a, bearer(iss.Token(testResource, testEmail, scope)))
		if rec.Code != http.StatusForbidden || reached != nil {
			t.Fatalf("scope %q: status = %d, handler reached = %v; want 403 and not reached", scope, rec.Code, reached != nil)
		}
		const want = `Bearer error="insufficient_scope", error_description="This resource needs the watchlist:read scope.", ` +
			`resource_metadata="https://api.example.test/.well-known/oauth-protected-resource/mcp", scope="watchlist:read watchlist:write"`
		if got := rec.Header().Get("WWW-Authenticate"); got != want {
			t.Errorf("scope %q: WWW-Authenticate =\n%s\nwant\n%s", scope, got, want)
		}
	}
}

func TestRequireTokenPassesTheVerifiedTokenToTheSDK(t *testing.T) {
	iss := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)

	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		rec, reached := guarded(t, a, func(r *http.Request) {
			r.Header.Set("Authorization", scheme+" "+iss.Token(testResource, testEmail, ScopeWatchlistRead))
		})
		if rec.Code != http.StatusNoContent || reached == nil {
			t.Fatalf("%s: status = %d, handler reached = %v; body: %s", scheme, rec.Code, reached != nil, rec.Body)
		}
		if id, ok := MCPIdentityFrom(reached); !ok || id.Email != testEmail {
			t.Errorf("%s: identity = %+v, %v", scheme, id, ok)
		}
		if HasScope(reached, ScopeWatchlistWrite) {
			t.Errorf("%s: a read-only token gained the write scope", scheme)
		}
	}
}

func TestRequireTokenAnswers503WhenTheKeysCannotBeFetched(t *testing.T) {
	iss := authtest.NewIssuer(t)
	a := newTestAuth(t, iss)
	iss.Down.Store(true)

	rec, reached := guarded(t, a, bearer(iss.Token(testResource, testEmail, bothScopes)))
	if rec.Code != http.StatusServiceUnavailable || reached != nil {
		t.Fatalf("status = %d, handler reached = %v; want 503 and not reached", rec.Code, reached != nil)
	}
	if rec.Header().Get("WWW-Authenticate") != "" || rec.Header().Get("Retry-After") == "" {
		t.Errorf("headers = %v; want Retry-After and no challenge", rec.Header())
	}
}

func TestAllowOrigins(t *testing.T) {
	allowed := []string{"https://watchlistnotify.com", "https://api.example.test"}
	reached := false
	h := AllowOrigins(allowed, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusAccepted)
	}))
	serve := func(method string, headers map[string]string) *httptest.ResponseRecorder {
		reached = false
		req := httptest.NewRequest(method, "https://api.example.test/mcp", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	t.Run("no Origin header passes without CORS headers", func(t *testing.T) {
		rec := serve(http.MethodPost, nil)
		if rec.Code != http.StatusAccepted || !reached {
			t.Fatalf("status = %d, reached = %v", rec.Code, reached)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q, want none", got)
		}
	})

	t.Run("an allowed origin passes and may read the challenge", func(t *testing.T) {
		for _, origin := range allowed {
			rec := serve(http.MethodPost, map[string]string{"Origin": origin, "Sec-Fetch-Site": "cross-site"})
			if rec.Code != http.StatusAccepted || !reached {
				t.Fatalf("%s: status = %d, reached = %v", origin, rec.Code, reached)
			}
			h := rec.Header()
			if h.Get("Access-Control-Allow-Origin") != origin || h.Get("Vary") != "Origin" ||
				h.Get("Access-Control-Expose-Headers") != "WWW-Authenticate" {
				t.Errorf("%s: headers = %v", origin, h)
			}
			if got := h.Get("Access-Control-Allow-Credentials"); got != "" {
				t.Errorf("%s: Allow-Credentials = %q, want none", origin, got)
			}
		}
	})

	t.Run("a preflight from an allowed origin is answered here", func(t *testing.T) {
		rec := serve(http.MethodOptions, map[string]string{
			"Origin":                         "https://watchlistnotify.com",
			"Access-Control-Request-Method":  "POST",
			"Access-Control-Request-Headers": "authorization, content-type, mcp-protocol-version",
		})
		if rec.Code != http.StatusNoContent || reached {
			t.Fatalf("status = %d, reached = %v", rec.Code, reached)
		}
		h := rec.Header()
		if h.Get("Access-Control-Allow-Origin") != "https://watchlistnotify.com" || h.Get("Access-Control-Allow-Methods") != "POST" {
			t.Errorf("headers = %v", h)
		}
		for _, header := range []string{"Authorization", "Content-Type", "MCP-Protocol-Version", "Mcp-Method"} {
			if !strings.Contains(h.Get("Access-Control-Allow-Headers"), header) {
				t.Errorf("Allow-Headers = %q, want %s in it", h.Get("Access-Control-Allow-Headers"), header)
			}
		}
	})

	t.Run("any other origin is refused, whatever the method", func(t *testing.T) {
		for _, origin := range []string{
			"https://evil.example", "null", "http://watchlistnotify.com", "https://watchlistnotify.com.evil.example",
			"https://watchlistnotify.com:8443", "http://localhost:3000",
		} {
			for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodOptions} {
				rec := serve(method, map[string]string{"Origin": origin, "Access-Control-Request-Method": "POST"})
				if rec.Code != http.StatusForbidden || reached {
					t.Errorf("%s from %s: status = %d, reached = %v; want 403", method, origin, rec.Code, reached)
				}
				if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
					t.Errorf("%s from %s: Allow-Origin = %q", method, origin, got)
				}
			}
		}
	})

	t.Run("a cross-site browser request without an Origin header is refused", func(t *testing.T) {
		rec := serve(http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site"})
		if rec.Code != http.StatusForbidden || reached {
			t.Errorf("status = %d, reached = %v; want 403", rec.Code, reached)
		}
	})
}

// The REST API and the MCP endpoint share a tenant, a signing key and a host.
// What keeps a token for one from working on the other is the audience.
func TestRESTMiddlewareRefusesTokensForTheMCPResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iss := authtest.NewIssuer(t)
	guard, err := ensureValidToken(iss.URL(), testRESTAud)
	if err != nil {
		t.Fatalf("ensureValidToken: %v", err)
	}
	r := gin.New()
	r.GET("/api/me", guard, func(c *gin.Context) {
		email, _ := c.Get("user_email")
		c.String(http.StatusOK, "%v", email)
	})
	get := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	if rec := get(iss.Token(testRESTAud, testEmail, "openid")); rec.Code != http.StatusOK || rec.Body.String() != testEmail {
		t.Fatalf("a REST token: status = %d, body = %q; want 200 and the email", rec.Code, rec.Body)
	}
	if rec := get(iss.Token(testResource, testEmail, bothScopes)); rec.Code != http.StatusUnauthorized {
		t.Errorf("an MCP token on the REST API: status = %d, want 401", rec.Code)
	}
}
