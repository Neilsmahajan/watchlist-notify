package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	jose "gopkg.in/go-jose/go-jose.v2"
)

// fakeIssuer answers key fetches with whatever the test last set.
type fakeIssuer struct {
	answer  any
	err     error
	fetches int
}

func (f *fakeIssuer) fetch(context.Context) (any, error) {
	f.fetches++
	return f.answer, f.err
}

func keySet(ids ...string) *jose.JSONWebKeySet {
	set := &jose.JSONWebKeySet{}
	for _, id := range ids {
		set.Keys = append(set.Keys, jose.JSONWebKey{KeyID: id})
	}
	return set
}

func TestSigningKeysAreCachedAndRefreshed(t *testing.T) {
	issuer := &fakeIssuer{answer: keySet("a")}
	now := time.Unix(1_700_000_000, 0)
	keys := newSigningKeys(issuer.fetch)
	keys.now = func() time.Time { return now }

	for range 3 {
		got, err := keys.KeyFunc(context.Background())
		if err != nil || got != issuer.answer {
			t.Fatalf("KeyFunc = %v, %v; want the fetched set", got, err)
		}
	}
	if issuer.fetches != 1 {
		t.Errorf("fetches = %d, want 1 while the keys are fresh", issuer.fetches)
	}

	now = now.Add(jwksCacheTTL)
	issuer.answer = keySet("b")
	if got, err := keys.KeyFunc(context.Background()); err != nil || got != issuer.answer {
		t.Errorf("after the TTL: KeyFunc = %v, %v; want the new set", got, err)
	}
}

func TestSigningKeysSurviveABadAnswer(t *testing.T) {
	good := keySet("a")
	issuer := &fakeIssuer{answer: good}
	now := time.Unix(1_700_000_000, 0)
	keys := newSigningKeys(issuer.fetch)
	keys.now = func() time.Time { return now }
	if _, err := keys.KeyFunc(context.Background()); err != nil {
		t.Fatal(err)
	}

	bad := map[string]func(){
		// What an error document decodes to: a key set without keys.
		"an empty key set": func() { issuer.answer, issuer.err = keySet(), nil },
		"a failed fetch":   func() { issuer.answer, issuer.err = nil, errors.New("connection refused") },
		"not a key set":    func() { issuer.answer, issuer.err = "keys", nil },
	}
	for name, set := range bad {
		now = now.Add(jwksCacheTTL)
		set()
		before := issuer.fetches
		if got, err := keys.KeyFunc(context.Background()); err != nil || got != good {
			t.Errorf("%s: KeyFunc = %v, %v; want the last good keys", name, got, err)
		}
		// The failure is not retried at once, nor is it cached for the TTL.
		if _, err := keys.KeyFunc(context.Background()); err != nil || issuer.fetches != before+1 {
			t.Errorf("%s: fetches = %d, want %d: a failed fetch is left alone for a while", name, issuer.fetches, before+1)
		}
		now = now.Add(jwksRetryAfter)
		if _, _ = keys.KeyFunc(context.Background()); issuer.fetches != before+2 {
			t.Errorf("%s: fetches = %d, want %d: the fetch is retried", name, issuer.fetches, before+2)
		}
	}

	// The issuer recovers: its answer replaces the old keys.
	now = now.Add(jwksRetryAfter)
	recovered := keySet("b")
	issuer.answer, issuer.err = recovered, nil
	if got, err := keys.KeyFunc(context.Background()); err != nil || got != recovered {
		t.Errorf("after recovery: KeyFunc = %v, %v; want the new keys", got, err)
	}

	// Keys that could not be refreshed for too long are given up.
	issuer.answer, issuer.err = nil, errors.New("connection refused")
	now = now.Add(jwksMaxStale)
	var keysErr *keySetError
	if got, err := keys.KeyFunc(context.Background()); got != nil || !errors.As(err, &keysErr) {
		t.Errorf("stale beyond the limit: KeyFunc = %v, %v; want a keySetError", got, err)
	}
}

func TestSigningKeysReportFailureWithoutKeys(t *testing.T) {
	issuer := &fakeIssuer{answer: keySet()}
	keys := newSigningKeys(issuer.fetch)
	var keysErr *keySetError
	if got, err := keys.KeyFunc(context.Background()); got != nil || !errors.As(err, &keysErr) {
		t.Fatalf("KeyFunc = %v, %v; want a keySetError", got, err)
	}
	// With nothing to fall back on, the next request tries again at once.
	issuer.answer = keySet("a")
	if got, err := keys.KeyFunc(context.Background()); err != nil || got != issuer.answer || issuer.fetches != 2 {
		t.Errorf("KeyFunc = %v, %v after %d fetches; want the keys from a second fetch", got, err, issuer.fetches)
	}
}

func TestSigningKeysFetchOutlivesItsCaller(t *testing.T) {
	keys := newSigningKeys(func(ctx context.Context) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return keySet("a"), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := keys.KeyFunc(ctx); err != nil {
		t.Errorf("a caller that hung up made the fetch fail: %v", err)
	}
}
