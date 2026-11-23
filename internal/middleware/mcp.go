package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// This file guards the MCP endpoint. The endpoint is an OAuth 2.1 protected
// resource: a caller presents an access token that the Auth0 tenant issued
// for this resource and nothing else, and learns where to get one from the
// challenge on a 401 (RFC 9728, RFC 6750).

// The scopes of the MCP resource.
const (
	// ScopeWatchlistRead is needed to reach the MCP endpoint at all.
	ScopeWatchlistRead = "watchlist:read"
	// ScopeWatchlistWrite is needed, as well, by tools that change data.
	ScopeWatchlistWrite = "watchlist:write"
)

// MCPScopes are the scopes the MCP resource defines. Every challenge names
// all of them, so that a client asks for both the first time it sends the
// user to sign in instead of coming back for the second one later.
var MCPScopes = []string{ScopeWatchlistRead, ScopeWatchlistWrite}

// mcpClaims are the claims an MCP access token carries beyond the registered
// ones. The identity claims are namespaced: a tenant Action adds them, since
// an access token for a custom API has no standard email claim.
type mcpClaims struct {
	Scope string `json:"scope"`
	Email string `json:"https://watchlistnotify.com/email"`
	// EmailVerified decodes only from a JSON boolean. A token that carries
	// the string "true", or a number, does not parse and is refused.
	EmailVerified bool   `json:"https://watchlistnotify.com/email_verified"`
	Name          string `json:"https://watchlistnotify.com/name"`
	Picture       string `json:"https://watchlistnotify.com/picture"`
}

// Validate refuses a token that does not name a verified email address. The
// API finds an account by its email, so an address nobody has proven they
// own must never be enough to open one.
func (c *mcpClaims) Validate(context.Context) error {
	if strings.TrimSpace(c.Email) == "" {
		return errors.New("the email claim is missing")
	}
	if !c.EmailVerified {
		return errors.New("the email is not verified")
	}
	return nil
}

// MCPIdentity is who an MCP access token was issued to.
type MCPIdentity struct {
	Subject string // the tenant's user ID
	Email   string // verified
	Name    string
	Picture string
}

// identityKey is where MCPIdentity is kept in auth.TokenInfo.Extra.
const identityKey = "watchlistnotify/identity"

// MCPIdentityFrom returns the identity behind a verified MCP token.
func MCPIdentityFrom(info *auth.TokenInfo) (MCPIdentity, bool) {
	if info == nil {
		return MCPIdentity{}, false
	}
	id, ok := info.Extra[identityKey].(MCPIdentity)
	return id, ok && id.Email != ""
}

// HasScope reports whether a verified token was granted scope.
func HasScope(info *auth.TokenInfo, scope string) bool {
	return info != nil && slices.Contains(info.Scopes, scope)
}

// ErrKeysUnavailable means a token could not be checked because the tenant's
// signing keys could not be fetched. The token may well be valid.
var ErrKeysUnavailable = errors.New("signing keys unavailable")

// MCPAuthConfig configures the guard of the MCP endpoint.
type MCPAuthConfig struct {
	// Issuer is the authorization server whose tokens are accepted.
	Issuer *url.URL
	// ResourceURL identifies the MCP endpoint. A token is accepted only if
	// its audience contains exactly this string (RFC 8707).
	ResourceURL string
	// MetadataURL is where the protected resource metadata is served. It is
	// sent in every challenge.
	MetadataURL string
	// JWKSOptions are passed to the JWKS provider; see newValidator.
	JWKSOptions []any
}

// MCPAuth verifies MCP access tokens and writes the challenges that tell a
// client how to obtain one.
type MCPAuth struct {
	validator   *validator.Validator
	metadataURL string
}

// NewMCPAuth returns the guard cfg describes.
func NewMCPAuth(cfg MCPAuthConfig) (*MCPAuth, error) {
	if cfg.Issuer == nil {
		return nil, errors.New("mcp auth: issuer is required")
	}
	if cfg.ResourceURL == "" {
		// The validator accepts a token when any expected audience is among
		// the token's, so an empty one must never be configured.
		return nil, errors.New("mcp auth: resource URL is required")
	}
	if cfg.MetadataURL == "" {
		return nil, errors.New("mcp auth: metadata URL is required")
	}
	v, err := newValidator(cfg.Issuer, cfg.ResourceURL, func() validator.CustomClaims {
		return &mcpClaims{}
	}, cfg.JWKSOptions...)
	if err != nil {
		return nil, fmt.Errorf("mcp auth: %w", err)
	}
	return &MCPAuth{validator: v, metadataURL: cfg.MetadataURL}, nil
}

// Verify checks an access token: the tenant's RS256 signature, the issuer,
// the time claims, that the audience contains this resource, and that the
// token names a verified email address. It is an auth.TokenVerifier.
//
// A token that fails any of these gives an error that wraps
// auth.ErrInvalidToken. When the signing keys cannot be fetched the error
// wraps ErrKeysUnavailable instead: that is this server's problem, and
// telling the client its token is bad would make it throw a good one away.
//
// Errors describe the failure and never contain the token.
func (a *MCPAuth) Verify(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	validated, err := a.validator.ValidateToken(ctx, token)
	if err != nil {
		var keys *keySetError
		if errors.As(err, &keys) {
			return nil, fmt.Errorf("%w: %v", ErrKeysUnavailable, keys.err)
		}
		return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}
	claims, ok := validated.(*validator.ValidatedClaims)
	if !ok {
		return nil, fmt.Errorf("%w: unexpected claims type", auth.ErrInvalidToken)
	}
	custom, ok := claims.CustomClaims.(*mcpClaims)
	if !ok {
		return nil, fmt.Errorf("%w: unexpected custom claims type", auth.ErrInvalidToken)
	}
	// The validator checks exp only when the claim is present. A token that
	// never expires is not one this server accepts.
	if claims.RegisteredClaims.Expiry == 0 {
		return nil, fmt.Errorf("%w: the token has no expiry", auth.ErrInvalidToken)
	}

	return &auth.TokenInfo{
		Scopes:     strings.Fields(custom.Scope),
		Expiration: time.Unix(claims.RegisteredClaims.Expiry, 0),
		UserID:     claims.RegisteredClaims.Subject,
		Extra: map[string]any{
			identityKey: MCPIdentity{
				Subject: claims.RegisteredClaims.Subject,
				Email:   strings.TrimSpace(custom.Email),
				Name:    custom.Name,
				Picture: custom.Picture,
			},
		},
	}, nil
}

type verifiedTokenKey struct{}

// RequireToken lets a request through to next only with a valid access token
// that grants ScopeWatchlistRead. Cookies are never read: the bearer token in
// the Authorization header is the only credential.
//
//   - No token: 401 with a challenge that names the metadata document and
//     the scopes to ask for.
//   - A token that does not verify: the same 401 with error="invalid_token".
//   - A valid token without the read scope: 403 with
//     error="insufficient_scope".
//   - Signing keys unavailable: 503.
//
// next finds the verified token where the MCP SDK looks for it, in
// auth.TokenInfoFromContext, and so do the tools it serves.
func (a *MCPAuth) RequireToken(next http.Handler) http.Handler {
	// The SDK keeps its context key private: its own bearer middleware is
	// the only thing that can put a TokenInfo where the transport reads it.
	// That middleware cannot write the challenges above (one scope list
	// doubles as "required" and "advertised", and a 403 carries no error
	// code), so the token is verified here, once, and the SDK's middleware is
	// handed the result instead of the token.
	planted := auth.RequireBearerToken(
		func(ctx context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
			info, _ := ctx.Value(verifiedTokenKey{}).(*auth.TokenInfo)
			if info == nil {
				return nil, auth.ErrInvalidToken
			}
			return info, nil
		},
		&auth.RequireBearerTokenOptions{ClockSkew: allowedClockSkew},
	)(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			a.challenge(w, http.StatusUnauthorized, "", "A bearer access token is required.")
			return
		}
		info, err := a.Verify(r.Context(), token, r)
		switch {
		case errors.Is(err, ErrKeysUnavailable):
			log.Printf("mcp auth: %v", err)
			w.Header().Set("Retry-After", "5")
			writeAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "The access token could not be checked. Try again shortly.")
		case err != nil:
			log.Printf("mcp auth: token refused: %v", err)
			a.challenge(w, http.StatusUnauthorized, "invalid_token", "The access token is invalid or has expired.")
		case !HasScope(info, ScopeWatchlistRead):
			a.DenyScope(w, "This resource needs the "+ScopeWatchlistRead+" scope.")
		default:
			ctx := context.WithValue(r.Context(), verifiedTokenKey{}, info)
			planted.ServeHTTP(w, r.WithContext(ctx))
		}
	})
}

// DenyScope answers a request whose token is valid but was not granted a
// scope the operation needs: 403 with error="insufficient_scope" and the
// scopes to ask for, which is what lets a client send the user back to
// consent to more (the specification's step-up flow).
func (a *MCPAuth) DenyScope(w http.ResponseWriter, description string) {
	a.challenge(w, http.StatusForbidden, "insufficient_scope", description)
}

// challenge writes a 401 or 403 with a WWW-Authenticate header per RFC 6750
// section 3 and RFC 9728 section 5.1. errorCode is empty when the request
// carried no credentials at all, as RFC 6750 asks.
func (a *MCPAuth) challenge(w http.ResponseWriter, status int, errorCode, description string) {
	var params []string
	if errorCode != "" {
		params = append(params,
			fmt.Sprintf("error=%q", errorCode),
			fmt.Sprintf("error_description=%q", description))
	}
	params = append(params,
		fmt.Sprintf("resource_metadata=%q", a.metadataURL),
		fmt.Sprintf("scope=%q", strings.Join(MCPScopes, " ")))
	w.Header().Set("WWW-Authenticate", "Bearer "+strings.Join(params, ", "))

	if errorCode == "" {
		errorCode = "unauthorized"
	}
	writeAuthError(w, status, errorCode, description)
}

func writeAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}

// bearerToken returns the token of an "Authorization: Bearer <token>" header.
// It applies the rule the SDK's middleware applies, so the two never disagree
// about whether a request carries one.
func bearerToken(r *http.Request) (string, bool) {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return "", false
	}
	return fields[1], true
}

// corsRequestHeaders are the request headers a browser client may send to
// the MCP endpoint: the bearer token, the JSON body's type, and the headers
// the Streamable HTTP transport defines.
const corsRequestHeaders = "Authorization, Content-Type, Accept, MCP-Protocol-Version, Mcp-Method, Mcp-Name, Mcp-Session-Id, Last-Event-ID"

// AllowOrigins applies the MCP endpoint's cross-origin policy in front of
// next.
//
// The Streamable HTTP transport requires a server to check the Origin header
// and answer 403 when it is present and not valid. "Valid" is left to the
// server; this one takes the strict reading and accepts only the origins in
// allowed. A request without an Origin header is let through: it does not
// come from a page in a browser, which is the only thing the header can
// protect against, and command-line and server-side MCP clients send none.
//
// An allowed origin gets the CORS headers it needs to call the endpoint with
// a bearer token, never Access-Control-Allow-Credentials: the endpoint
// ignores cookies, so a browser has no ambient authority to lend to a page.
//
// Go's own cross-origin protection runs behind the same list, so a request a
// browser marks cross-site in Sec-Fetch-Site is refused even when the Origin
// header is missing.
func AllowOrigins(allowed []string, next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	for _, origin := range allowed {
		// An origin that does not parse is not allowed, and stays that way.
		_ = protection.AddTrustedOrigin(origin)
	}
	next = protection.Handler(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !slices.Contains(allowed, origin) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			// A JSON-RPC error without an id, as the transport suggests.
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"Origin not allowed"}}` + "\n"))
			return
		}

		h := w.Header()
		h.Add("Vary", "Origin")
		h.Set("Access-Control-Allow-Origin", origin)
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "POST")
			h.Set("Access-Control-Allow-Headers", corsRequestHeaders)
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Without this a page could not read the challenge on a 401.
		h.Set("Access-Control-Expose-Headers", "WWW-Authenticate")
		next.ServeHTTP(w, r)
	})
}
