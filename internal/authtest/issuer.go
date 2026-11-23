// Package authtest stands in for the Auth0 tenant in tests: an HTTP server
// that publishes a signing key the way an OpenID provider does, and mints
// access tokens signed with it.
package authtest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/go-jose/go-jose.v2"
	"gopkg.in/go-jose/go-jose.v2/jwt"
)

// The namespaced claims the tenant's Action adds to an access token.
const (
	ClaimEmail         = "https://watchlistnotify.com/email"
	ClaimEmailVerified = "https://watchlistnotify.com/email_verified"
	ClaimName          = "https://watchlistnotify.com/name"
	ClaimPicture       = "https://watchlistnotify.com/picture"
)

const keyID = "test-key"

// Issuer is a fake authorization server.
type Issuer struct {
	t      testing.TB
	server *httptest.Server
	key    *rsa.PrivateKey
	// Down, when set, makes the key endpoints answer 500.
	Down atomic.Bool
	// KeyFetches counts requests for the key set.
	KeyFetches atomic.Int64
}

// NewIssuer starts an issuer that stops when the test ends.
func NewIssuer(t testing.TB) *Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	iss := &Issuer{t: t, key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		if iss.Down.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{
			"issuer":   iss.URL().String(),
			"jwks_uri": iss.server.URL + "/.well-known/jwks.json",
		})
	})
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		iss.KeyFetches.Add(1)
		if iss.Down.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: keyID, Algorithm: string(jose.RS256), Use: "sig"},
		}})
	})
	iss.server = httptest.NewServer(mux)
	t.Cleanup(iss.server.Close)
	return iss
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// URL is the issuer identifier, with the trailing slash Auth0 puts in the
// iss claim.
func (i *Issuer) URL() *url.URL {
	u, err := url.Parse(i.server.URL + "/")
	if err != nil {
		i.t.Fatalf("issuer url: %v", err)
	}
	return u
}

// Claims is the payload of a token. Mutate what Token returns it from to
// build the token a test needs.
type Claims map[string]any

// Claims returns the claims of a token like the ones the tenant issues to a
// signed-in user with a verified email: for audience, with the given scope,
// valid for five minutes.
func (i *Issuer) Claims(audience, email, scope string) Claims {
	now := time.Now()
	return Claims{
		"iss":              i.URL().String(),
		"sub":              "auth0|" + email,
		"aud":              []string{audience, i.URL().String() + "userinfo"},
		"iat":              now.Unix(),
		"exp":              now.Add(5 * time.Minute).Unix(),
		"scope":            scope,
		ClaimEmail:         email,
		ClaimEmailVerified: true,
		ClaimName:          "Test User",
		ClaimPicture:       "https://example.test/picture.png",
	}
}

// Token returns a signed token for the default Claims.
func (i *Issuer) Token(audience, email, scope string) string {
	return i.Sign(i.Claims(audience, email, scope))
}

// Sign returns claims as an RS256 token signed with the issuer's key.
func (i *Issuer) Sign(claims Claims) string {
	return i.signWith(i.key, claims)
}

// SignWithOtherKey signs claims with a key the issuer does not publish,
// under the published key's ID: a forged token.
func (i *Issuer) SignWithOtherKey(claims Claims) string {
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		i.t.Fatalf("generate key: %v", err)
	}
	return i.signWith(other, claims)
}

func (i *Issuer) signWith(key *rsa.PrivateKey, claims Claims) string {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID),
	)
	if err != nil {
		i.t.Fatalf("new signer: %v", err)
	}
	return i.serialize(signer, claims)
}

// SignHS256 signs claims with HMAC, using the issuer's public key as the
// secret: the classic algorithm-confusion forgery.
func (i *Issuer) SignHS256(claims Claims) string {
	secret := i.key.PublicKey.N.Bytes()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: secret},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID),
	)
	if err != nil {
		i.t.Fatalf("new signer: %v", err)
	}
	return i.serialize(signer, claims)
}

func (i *Issuer) serialize(signer jose.Signer, claims Claims) string {
	token, err := jwt.Signed(signer).Claims(map[string]any(claims)).CompactSerialize()
	if err != nil {
		i.t.Fatalf("sign token: %v", err)
	}
	return token
}
