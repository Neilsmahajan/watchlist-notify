// Package availability keeps the shared `titles` collection in step with
// TMDb: it turns TMDb watch-provider responses into stored snapshots, diffs
// each new snapshot against the stored one, and records what changed as
// availability events.
package availability

import (
	"fmt"
	"sort"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
)

// BuildSnapshot converts TMDb's provider lists for one region into a stored
// snapshot. fetchedAt is truncated to milliseconds, the precision MongoDB
// keeps, because it is later compared for equality as the snapshot's version.
func BuildSnapshot(rp tmdb.RegionProviders, fetchedAt time.Time) models.RegionSnapshot {
	snap := models.RegionSnapshot{
		Link:      rp.Link,
		Offers:    []models.ProviderOffer{},
		FetchedAt: fetchedAt.UTC().Truncate(time.Millisecond),
	}
	type offerKey struct {
		providerID int
		access     string
	}
	seen := make(map[offerKey]bool)
	add := func(access string, providers []tmdb.Provider) {
		for _, p := range providers {
			key := offerKey{p.ProviderID, access}
			if seen[key] {
				continue
			}
			seen[key] = true
			code, _ := models.MapProvider(p.ProviderID, p.ProviderName)
			snap.Offers = append(snap.Offers, models.ProviderOffer{
				ProviderID:   p.ProviderID,
				ProviderName: p.ProviderName,
				LogoPath:     p.LogoPath,
				Code:         code,
				Access:       access,
			})
		}
	}
	add(models.AccessSubscription, rp.Flatrate)
	add(models.AccessFree, rp.Free)
	add(models.AccessAds, rp.Ads)
	add(models.AccessRent, rp.Rent)
	add(models.AccessBuy, rp.Buy)
	return snap
}

// Change is one service starting or stopping to stream a title.
type Change struct {
	Kind         string // models.ChangeAdded | models.ChangeRemoved
	ServiceCode  string
	ProviderName string
	Access       string
}

// Diff compares the stored snapshot with a fresh one at the level of our
// service codes, counting streaming access only.
//
//   - prev == nil is a title's first snapshot for the region: a baseline. It
//     yields no changes, so a newly tracked title (or a whole newly tracked
//     catalog) never looks like "everything just arrived".
//   - A service is added when its code streams the title in next and not in
//     prev, and removed in the opposite case.
//   - A service that only changes access type (subscription to ads), provider
//     variant ("Paramount Plus Premium" to "Essential") or rent/buy offers is
//     not a change.
//
// Both sides are resolved through the current mapping table rather than the
// codes stored with the snapshot, so correcting the table never produces a
// burst of "added" events.
func Diff(prev *models.RegionSnapshot, next models.RegionSnapshot) []Change {
	if prev == nil {
		return nil
	}
	before := servicesByCode(*prev)
	after := servicesByCode(next)

	var changes []Change
	for code, svc := range after {
		if _, ok := before[code]; !ok {
			changes = append(changes, Change{
				Kind:         models.ChangeAdded,
				ServiceCode:  code,
				ProviderName: svc.ProviderName,
				Access:       svc.BestAccess(),
			})
		}
	}
	for code, svc := range before {
		if _, ok := after[code]; !ok {
			changes = append(changes, Change{
				Kind:         models.ChangeRemoved,
				ServiceCode:  code,
				ProviderName: svc.ProviderName,
				Access:       svc.BestAccess(),
			})
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].ServiceCode < changes[j].ServiceCode
	})
	return changes
}

func servicesByCode(snap models.RegionSnapshot) map[string]models.ServiceAvailability {
	services := snap.Services()
	byCode := make(map[string]models.ServiceAvailability, len(services))
	for _, svc := range services {
		byCode[svc.Code] = svc
	}
	return byCode
}

// EventID is the deterministic identity of a change: the title, region,
// service and kind, plus the version (fetch time) of the stored snapshot the
// change was measured against. Two diffs against the same stored snapshot,
// whether a retry or two overlapping job runs, therefore produce the same ID
// and the second insert is a no-op.
func EventID(titleKey, region string, c Change, prevVersion time.Time) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d", titleKey, region, c.ServiceCode, c.Kind, prevVersion.UnixMilli())
}

// Events turns the differences between prev and next into event documents.
// It returns nil for a baseline.
func Events(ref models.TitleRef, region string, prev *models.RegionSnapshot, next models.RegionSnapshot) []models.AvailabilityEvent {
	changes := Diff(prev, next)
	if len(changes) == 0 {
		return nil
	}
	key := ref.Key()
	events := make([]models.AvailabilityEvent, 0, len(changes))
	for _, c := range changes {
		events = append(events, models.AvailabilityEvent{
			ID:           EventID(key, region, c, prev.FetchedAt),
			TitleKey:     key,
			Region:       region,
			ServiceCode:  c.ServiceCode,
			ProviderName: c.ProviderName,
			Access:       c.Access,
			Change:       c.Kind,
			DetectedAt:   next.FetchedAt,
			ExpiresAt:    next.FetchedAt.Add(models.EventRetention),
		})
	}
	return events
}
