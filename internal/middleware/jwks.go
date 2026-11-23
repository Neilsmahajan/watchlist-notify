package middleware

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	jose "gopkg.in/go-jose/go-jose.v2"
)

const (
	// jwksCacheTTL is how long the tenant's signing keys are used before
	// they are fetched again.
	jwksCacheTTL = 5 * time.Minute
	// jwksRetryAfter is how long a failed refresh is left alone before the
	// next try, while the previous keys are still in use.
	jwksRetryAfter = 15 * time.Second
	// jwksMaxStale is how long keys that could not be refreshed are still
	// used. Signing keys change rarely and a rotated-out key stays published
	// for a while, so keys an hour old are far more likely right than not.
	jwksMaxStale = time.Hour
	// jwksFetchTimeout bounds one fetch of the keys.
	jwksFetchTimeout = 10 * time.Second
)

// signingKeys caches an issuer's signing keys.
//
// It differs from the caching provider of the JWT library in what it does
// with a bad answer. That provider caches whatever decodes as JSON, without
// looking at the HTTP status: an error document such as
// {"error":"temporarily_unavailable"} decodes as a key set with no keys, and
// for the next five minutes every valid token fails its signature check and
// is answered with a 401 that tells the client to throw it away. Here a
// fetch that fails, or that yields no keys, never replaces keys that worked:
// they stay in use (for up to jwksMaxStale) while the fetch is retried, and
// only when there are no usable keys at all is the failure reported, as a
// keySetError, which the callers turn into "try again" and not "bad token".
type signingKeys struct {
	// fetch gets the current key set from the issuer.
	fetch func(ctx context.Context) (any, error)
	now   func() time.Time

	mu        sync.Mutex
	keys      *jose.JSONWebKeySet
	fetchedAt time.Time // when keys was fetched
	failedAt  time.Time // when the last fetch failed; zero after a success
	failure   error     // why
}

func newSigningKeys(fetch func(ctx context.Context) (any, error)) *signingKeys {
	return &signingKeys{fetch: fetch, now: time.Now}
}

// KeyFunc returns the key set to verify a token with. It has the signature
// the validator wants.
func (k *signingKeys) KeyFunc(ctx context.Context) (any, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	now := k.now()
	if k.keys != nil && now.Sub(k.fetchedAt) < jwksCacheTTL {
		return k.keys, nil
	}
	// While older keys can stand in, a failed fetch is left alone for a
	// moment. With no keys at all every request tries again: there is
	// nothing to answer with otherwise, and requests wait their turn here,
	// so the issuer sees one fetch at a time.
	if k.keys != nil && !k.failedAt.IsZero() && now.Sub(k.failedAt) < jwksRetryAfter {
		return k.fallback(now)
	}

	// The fetch serves every later request, so it must not die with the one
	// that happened to start it: a caller who hangs up would otherwise be
	// able to make it fail.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jwksFetchTimeout)
	defer cancel()
	set, err := k.fetchSet(fetchCtx)
	if err != nil {
		k.failedAt, k.failure = now, err
		log.Printf("jwks: fetching signing keys failed: %v", err)
		return k.fallback(now)
	}
	k.keys, k.fetchedAt = set, now
	k.failedAt, k.failure = time.Time{}, nil
	return set, nil
}

// fallback answers when no fresh keys could be had: with the last keys that
// worked while they are not too old, with the failure otherwise.
func (k *signingKeys) fallback(now time.Time) (any, error) {
	if k.keys != nil && now.Sub(k.fetchedAt) < jwksMaxStale {
		return k.keys, nil
	}
	return nil, &keySetError{err: k.failure}
}

func (k *signingKeys) fetchSet(ctx context.Context) (*jose.JSONWebKeySet, error) {
	fetched, err := k.fetch(ctx)
	if err != nil {
		return nil, err
	}
	set, ok := fetched.(*jose.JSONWebKeySet)
	if !ok || set == nil {
		return nil, errors.New("the issuer's answer is not a key set")
	}
	if len(set.Keys) == 0 {
		return nil, errors.New("the issuer's key set is empty")
	}
	return set, nil
}
