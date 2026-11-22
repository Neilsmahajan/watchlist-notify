package availability

import (
	"reflect"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
)

var t0 = time.Date(2026, 3, 1, 6, 0, 0, 0, time.UTC)

func offer(id int, name, access string) models.ProviderOffer {
	return models.ProviderOffer{ProviderID: id, ProviderName: name, Access: access}
}

func snapshot(at time.Time, offers ...models.ProviderOffer) models.RegionSnapshot {
	return models.RegionSnapshot{FetchedAt: at, Offers: offers}
}

func TestDiff(t *testing.T) {
	var (
		netflix      = offer(8, "Netflix", models.AccessSubscription)
		netflixAds   = offer(8, "Netflix", models.AccessAds)
		netflixRent  = offer(8, "Netflix", models.AccessRent)
		hulu         = offer(15, "Hulu", models.AccessSubscription)
		paraPremium  = offer(2303, "Paramount Plus Premium", models.AccessSubscription)
		paraEssent   = offer(2616, "Paramount Plus Essential", models.AccessSubscription)
		paraChannel  = offer(999010, "Paramount+ Amazon Channel", models.AccessSubscription)
		kanopy       = offer(999020, "Kanopy", models.AccessFree)
		amazonBuy    = offer(10, "Amazon Video", models.AccessBuy)
		added        = func(code string) Change { return Change{Kind: models.ChangeAdded, ServiceCode: code} }
		removed      = func(code string) Change { return Change{Kind: models.ChangeRemoved, ServiceCode: code} }
		prevSnapshot = func(offers ...models.ProviderOffer) *models.RegionSnapshot {
			s := snapshot(t0, offers...)
			return &s
		}
	)

	tests := []struct {
		name string
		prev *models.RegionSnapshot
		next models.RegionSnapshot
		want []Change
	}{
		{
			name: "first snapshot is a baseline",
			prev: nil,
			next: snapshot(t0, netflix, hulu),
			want: nil,
		},
		{
			name: "nothing changed",
			prev: prevSnapshot(netflix),
			next: snapshot(t0, netflix),
			want: nil,
		},
		{
			name: "service added",
			prev: prevSnapshot(netflix),
			next: snapshot(t0, netflix, hulu),
			want: []Change{added(models.ServiceHulu)},
		},
		{
			name: "service removed",
			prev: prevSnapshot(netflix, hulu),
			next: snapshot(t0, hulu),
			want: []Change{removed(models.ServiceNetflix)},
		},
		{
			name: "added sorts before removed, then by code",
			prev: prevSnapshot(netflix),
			next: snapshot(t0, paraPremium, hulu),
			want: []Change{added(models.ServiceHulu), added(models.ServiceParamountPlus), removed(models.ServiceNetflix)},
		},
		{
			name: "everything gone from a successful fetch is a removal",
			prev: prevSnapshot(netflix),
			next: snapshot(t0),
			want: []Change{removed(models.ServiceNetflix)},
		},
		{
			name: "access type change is not an addition",
			prev: prevSnapshot(netflix),
			next: snapshot(t0, netflixAds),
			want: nil,
		},
		{
			name: "plan variant change is not an addition",
			prev: prevSnapshot(paraPremium),
			next: snapshot(t0, paraEssent),
			want: nil,
		},
		{
			name: "gaining a second variant is not an addition",
			prev: prevSnapshot(paraPremium),
			next: snapshot(t0, paraPremium, paraEssent),
			want: nil,
		},
		{
			name: "rent and buy offers are ignored",
			prev: prevSnapshot(hulu),
			next: snapshot(t0, hulu, netflixRent, amazonBuy),
			want: nil,
		},
		{
			name: "losing only a rent offer is not a removal",
			prev: prevSnapshot(hulu, netflixRent),
			next: snapshot(t0, hulu),
			want: nil,
		},
		{
			name: "rent to subscription is an addition",
			prev: prevSnapshot(netflixRent),
			next: snapshot(t0, netflix),
			want: []Change{added(models.ServiceNetflix)},
		},
		{
			name: "resale channels and unknown providers are ignored",
			prev: prevSnapshot(hulu),
			next: snapshot(t0, hulu, paraChannel, kanopy),
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Diff(tt.prev, tt.next)
			// The cases only pin down what changed, not the descriptive fields.
			for i := range got {
				got[i].ProviderName, got[i].Access = "", ""
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Diff() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A snapshot stored before a provider was mapped carries no code for it. When
// the mapping table learns the provider, both sides of the diff resolve it the
// same way, so the fix does not announce the title as newly arrived.
func TestDiffUsesCurrentMappingForStoredSnapshots(t *testing.T) {
	stale := offer(8, "Netflix", models.AccessSubscription)
	stale.Code = "" // as if ID 8 was unknown when this was stored
	prev := snapshot(t0, stale)

	fresh := BuildSnapshot(tmdb.RegionProviders{
		Flatrate: []tmdb.Provider{{ProviderID: 8, ProviderName: "Netflix"}},
	}, t0.Add(24*time.Hour))

	if got := Diff(&prev, fresh); got != nil {
		t.Errorf("Diff() = %+v, want no changes", got)
	}
}

func TestBuildSnapshot(t *testing.T) {
	at := time.Date(2026, 3, 1, 6, 0, 0, 123456789, time.FixedZone("EST", -5*3600))
	snap := BuildSnapshot(tmdb.RegionProviders{
		Link: "https://www.themoviedb.org/movie/603/watch?locale=US",
		Flatrate: []tmdb.Provider{
			{ProviderID: 8, ProviderName: "Netflix", LogoPath: "/n.jpg"},
			{ProviderID: 8, ProviderName: "Netflix", LogoPath: "/n.jpg"}, // TMDb repeats entries now and then
			{ProviderID: 999010, ProviderName: "Paramount+ Amazon Channel"},
		},
		Ads:  []tmdb.Provider{{ProviderID: 73, ProviderName: "Tubi TV"}},
		Rent: []tmdb.Provider{{ProviderID: 2, ProviderName: "Apple TV Store"}},
		Buy:  []tmdb.Provider{{ProviderID: 2, ProviderName: "Apple TV Store"}},
	}, at)

	want := []models.ProviderOffer{
		{ProviderID: 8, ProviderName: "Netflix", LogoPath: "/n.jpg", Code: models.ServiceNetflix, Access: models.AccessSubscription},
		{ProviderID: 999010, ProviderName: "Paramount+ Amazon Channel", Access: models.AccessSubscription},
		{ProviderID: 73, ProviderName: "Tubi TV", Code: models.ServiceTubi, Access: models.AccessAds},
		{ProviderID: 2, ProviderName: "Apple TV Store", Access: models.AccessRent},
		{ProviderID: 2, ProviderName: "Apple TV Store", Access: models.AccessBuy},
	}
	if !reflect.DeepEqual(snap.Offers, want) {
		t.Errorf("Offers = %+v\nwant %+v", snap.Offers, want)
	}
	if wantAt := time.Date(2026, 3, 1, 11, 0, 0, 123000000, time.UTC); !snap.FetchedAt.Equal(wantAt) || snap.FetchedAt.Location() != time.UTC {
		t.Errorf("FetchedAt = %v, want %v (UTC, millisecond precision)", snap.FetchedAt, wantAt)
	}

	if empty := BuildSnapshot(tmdb.RegionProviders{}, at); empty.Offers == nil || len(empty.Offers) != 0 {
		t.Errorf("empty region Offers = %#v, want an empty non-nil slice", empty.Offers)
	}
}

func TestEvents(t *testing.T) {
	ref := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 603}
	prev := snapshot(t0, offer(8, "Netflix", models.AccessSubscription))
	next := snapshot(t0.Add(24*time.Hour), offer(15, "Hulu", models.AccessAds))

	if got := Events(ref, "US", nil, next); got != nil {
		t.Fatalf("Events() for a baseline = %+v, want nil", got)
	}

	events := Events(ref, "US", &prev, next)
	if len(events) != 2 {
		t.Fatalf("Events() returned %d events, want 2: %+v", len(events), events)
	}
	want := models.AvailabilityEvent{
		ID:           "movie:603|US|hulu|added|" + "1772344800000",
		TitleKey:     "movie:603",
		Region:       "US",
		ServiceCode:  models.ServiceHulu,
		ProviderName: "Hulu",
		Access:       models.AccessAds,
		Change:       models.ChangeAdded,
		DetectedAt:   next.FetchedAt,
		ExpiresAt:    next.FetchedAt.Add(models.EventRetention),
	}
	if events[0] != want {
		t.Errorf("events[0] = %+v\nwant      %+v", events[0], want)
	}
	if events[1].Change != models.ChangeRemoved || events[1].ServiceCode != models.ServiceNetflix {
		t.Errorf("events[1] = %+v, want netflix removed", events[1])
	}

	// The ID depends on what the change was measured against, not on when the
	// measurement ran: a retry a minute later yields the same IDs.
	retry := next
	retry.FetchedAt = next.FetchedAt.Add(time.Minute)
	again := Events(ref, "US", &prev, retry)
	for i := range events {
		if again[i].ID != events[i].ID {
			t.Errorf("retry produced ID %q, want %q", again[i].ID, events[i].ID)
		}
	}

	// Against a different stored version the same change is a new event.
	later := prev
	later.FetchedAt = prev.FetchedAt.Add(48 * time.Hour)
	if other := Events(ref, "US", &later, next); other[0].ID == events[0].ID {
		t.Error("events measured against different snapshot versions share an ID")
	}
}
