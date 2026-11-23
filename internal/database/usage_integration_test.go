package database

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// These tests pin what the spend limits rely on MongoDB for: a reservation is
// one conditional write, so the limit holds whatever the interleaving.

func TestReserveUsageStopsAtTheLimit(t *testing.T) {
	srv := New()
	ctx := context.Background()
	const key = "it-usage:stops-at-limit"
	expires := time.Now().Add(time.Hour)

	for want := 1; want <= 3; want++ {
		count, ok, err := srv.ReserveUsage(ctx, key, 3, expires)
		if err != nil || !ok || count != want {
			t.Fatalf("reservation %d = (%d, %v, %v), want (%d, true, nil)", want, count, ok, err, want)
		}
	}
	for range 2 {
		count, ok, err := srv.ReserveUsage(ctx, key, 3, expires)
		if err != nil || ok || count != 0 {
			t.Fatalf("reservation past the limit = (%d, %v, %v), want (0, false, nil)", count, ok, err)
		}
	}
	counts, err := srv.UsageCounts(ctx, []string{key, "it-usage:never-used"})
	if err != nil {
		t.Fatalf("UsageCounts: %v", err)
	}
	if counts[key] != 3 {
		t.Errorf("count = %d, want 3: a refused reservation must not count", counts[key])
	}
	if _, present := counts["it-usage:never-used"]; present {
		t.Errorf("a counter that was never used is in the result: %v", counts)
	}
}

func TestReserveUsageWithNoBudgetWritesNothing(t *testing.T) {
	srv := New()
	ctx := context.Background()
	const key = "it-usage:no-budget"
	for _, limit := range []int{0, -1} {
		if count, ok, err := srv.ReserveUsage(ctx, key, limit, time.Now().Add(time.Hour)); err != nil || ok || count != 0 {
			t.Fatalf("limit %d: reservation = (%d, %v, %v), want (0, false, nil)", limit, count, ok, err)
		}
	}
	counts, err := srv.UsageCounts(ctx, []string{key})
	if err != nil {
		t.Fatalf("UsageCounts: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("a limit of zero created a counter: %v", counts)
	}
}

// Many requests at once, on a counter that does not exist yet: the first
// reservations race to create the document, the rest race for what is left.
func TestReserveUsageHoldsTheLimitUnderConcurrency(t *testing.T) {
	srv := New()
	ctx := context.Background()
	const (
		key     = "it-usage:concurrent"
		limit   = 7
		callers = 40
	)
	expires := time.Now().Add(time.Hour)

	var granted atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	start := make(chan struct{})
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, ok, err := srv.ReserveUsage(ctx, key, limit, expires)
			if err != nil {
				errs <- err
				return
			}
			if ok {
				granted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("ReserveUsage: %v", err)
	}

	if got := granted.Load(); got != limit {
		t.Errorf("%d of %d concurrent reservations were granted, want exactly %d", got, callers, limit)
	}
	counts, err := srv.UsageCounts(ctx, []string{key})
	if err != nil {
		t.Fatalf("UsageCounts: %v", err)
	}
	if counts[key] != limit {
		t.Errorf("count = %d, want %d", counts[key], limit)
	}
}

func TestReleaseUsage(t *testing.T) {
	srv := New()
	ctx := context.Background()
	const key = "it-usage:release"
	expires := time.Now().Add(time.Hour)

	// Releasing a counter that does not exist creates nothing.
	if err := srv.ReleaseUsage(ctx, key); err != nil {
		t.Fatalf("ReleaseUsage on a missing counter: %v", err)
	}
	if counts, err := srv.UsageCounts(ctx, []string{key}); err != nil || len(counts) != 0 {
		t.Fatalf("after releasing a missing counter: %v, %v", counts, err)
	}

	for range 2 {
		if _, ok, err := srv.ReserveUsage(ctx, key, 2, expires); err != nil || !ok {
			t.Fatalf("ReserveUsage: %v, %v", ok, err)
		}
	}
	if _, ok, _ := srv.ReserveUsage(ctx, key, 2, expires); ok {
		t.Fatal("a third reservation was granted with a limit of 2")
	}
	if err := srv.ReleaseUsage(ctx, key); err != nil {
		t.Fatalf("ReleaseUsage: %v", err)
	}
	// The released place can be taken again, once.
	if count, ok, err := srv.ReserveUsage(ctx, key, 2, expires); err != nil || !ok || count != 2 {
		t.Fatalf("reservation after a release = (%d, %v, %v), want (2, true, nil)", count, ok, err)
	}

	// A counter never goes below zero.
	for range 5 {
		if err := srv.ReleaseUsage(ctx, key); err != nil {
			t.Fatalf("ReleaseUsage: %v", err)
		}
	}
	counts, err := srv.UsageCounts(ctx, []string{key})
	if err != nil {
		t.Fatalf("UsageCounts: %v", err)
	}
	if counts[key] != 0 {
		t.Errorf("count after releasing more than was reserved = %d, want 0", counts[key])
	}
}

// The expiry is what the TTL index works from: it is written when the
// counter is created and later reservations leave it alone.
func TestReserveUsageStoresTheExpiryOnce(t *testing.T) {
	srv := New().(*service)
	ctx := context.Background()
	const key = "it-usage:expiry"
	first := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)

	if _, ok, err := srv.ReserveUsage(ctx, key, 5, first); err != nil || !ok {
		t.Fatalf("ReserveUsage: %v, %v", ok, err)
	}
	if _, ok, err := srv.ReserveUsage(ctx, key, 5, first.Add(48*time.Hour)); err != nil || !ok {
		t.Fatalf("ReserveUsage: %v, %v", ok, err)
	}

	var stored usageCounter
	err := srv.db.Database(databaseName).Collection(collUsage).FindOne(ctx, bson.M{"_id": key}).Decode(&stored)
	if err != nil {
		t.Fatalf("read the counter: %v", err)
	}
	if stored.Count != 2 || !stored.ExpiresAt.Equal(first) {
		t.Errorf("stored counter = %+v, want count 2 expiring at %s", stored, first)
	}
}
