package digest

import (
	"reflect"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const matrixKey = "movie:603"

func subscriber(codes ...string) *models.User {
	u := &models.User{ID: primitive.NewObjectID(), Email: "viewer@example.com", Region: "US"}
	for _, code := range codes {
		u.Services = append(u.Services, models.ServiceSubscription{Code: code, Active: true})
	}
	u.Preferences.DigestConsent = true
	u.Preferences.Digest = models.DigestSettings{Enabled: true, Interval: 1, IntervalUnit: models.DigestUnitWeeks}
	return u
}

func matrixItem(addedAt time.Time) *models.WatchlistItem {
	return &models.WatchlistItem{
		ID:      primitive.NewObjectID(),
		Title:   "The Matrix",
		Type:    models.WatchlistTypeMovie,
		Year:    1999,
		TMDbID:  603,
		Status:  models.WatchlistStatusPlanned,
		AddedAt: addedAt,
	}
}

// streamingOn builds a stored title currently streaming on the given services.
func streamingOn(key string, codes ...string) *models.Title {
	ids := map[string]int{
		models.ServiceNetflix: 8, models.ServiceHulu: 15, models.ServiceMax: 1899, models.ServiceDisneyPlus: 337,
	}
	snap := models.RegionSnapshot{FetchedAt: now, Offers: []models.ProviderOffer{}}
	for _, code := range codes {
		snap.Offers = append(snap.Offers, models.ProviderOffer{
			ProviderID: ids[code], ProviderName: models.ServiceName(code), Access: models.AccessSubscription,
		})
	}
	return &models.Title{
		ID:            key,
		TitleMetadata: models.TitleMetadata{PosterPath: "/poster.jpg", Year: 1999},
		Providers:     map[string]models.RegionSnapshot{"US": snap},
	}
}

func event(change, code string, at time.Time) models.AvailabilityEvent {
	return models.AvailabilityEvent{
		ID:          matrixKey + "|US|" + code + "|" + change + "|" + at.String(),
		TitleKey:    matrixKey,
		Region:      "US",
		ServiceCode: code,
		Change:      change,
		DetectedAt:  at,
	}
}

func added(code string, at time.Time) models.AvailabilityEvent {
	return event(models.ChangeAdded, code, at)
}

func removed(code string, at time.Time) models.AvailabilityEvent {
	return event(models.ChangeRemoved, code, at)
}

func TestSelect(t *testing.T) {
	windowStart := day(-7)

	// base is a digest with exactly one qualifying title: The Matrix, on the
	// user's list for a month, arrived on Netflix yesterday and is still there.
	base := func() Selection {
		return Selection{
			User:        subscriber(models.ServiceNetflix, models.ServiceHulu),
			Region:      "US",
			Items:       []*models.WatchlistItem{matrixItem(day(-30))},
			Titles:      map[string]*models.Title{matrixKey: streamingOn(matrixKey, models.ServiceNetflix)},
			Events:      []models.AvailabilityEvent{added(models.ServiceNetflix, day(-1))},
			WindowStart: windowStart,
			Now:         now,
		}
	}

	tests := []struct {
		name   string
		change func(*Selection)
		want   []string // service codes of the single expected entry; nil for no entry
	}{
		{
			name:   "newly available on an active service",
			change: func(*Selection) {},
			want:   []string{models.ServiceNetflix},
		},
		{
			name:   "detected before the window",
			change: func(s *Selection) { s.Events = []models.AvailabilityEvent{added(models.ServiceNetflix, day(-8))} },
		},
		{
			name:   "detected exactly at the window start belongs to the previous digest",
			change: func(s *Selection) { s.Events = []models.AvailabilityEvent{added(models.ServiceNetflix, windowStart)} },
		},
		{
			name:   "detected exactly now is included",
			change: func(s *Selection) { s.Events = []models.AvailabilityEvent{added(models.ServiceNetflix, now)} },
			want:   []string{models.ServiceNetflix},
		},
		{
			name: "detected after this run started is left for the next digest",
			change: func(s *Selection) {
				s.Events = []models.AvailabilityEvent{added(models.ServiceNetflix, now.Add(time.Second))}
			},
		},
		{
			name:   "added to the list after it became available",
			change: func(s *Selection) { s.Items[0].AddedAt = day(-1).Add(time.Hour) },
		},
		{
			name:   "added to the list at the moment of detection",
			change: func(s *Selection) { s.Items[0].AddedAt = day(-1) },
		},
		{
			name: "no longer on the service",
			change: func(s *Selection) {
				s.Titles[matrixKey] = streamingOn(matrixKey, models.ServiceMax)
			},
		},
		{
			name:   "already finished",
			change: func(s *Selection) { s.Items[0].Status = models.WatchlistStatusFinished },
		},
		{
			name:   "currently watching still counts",
			change: func(s *Selection) { s.Items[0].Status = models.WatchlistStatusWatching },
			want:   []string{models.ServiceNetflix},
		},
		{
			name: "service the user does not have",
			change: func(s *Selection) {
				s.Events = []models.AvailabilityEvent{added(models.ServiceDisneyPlus, day(-1))}
				s.Titles[matrixKey] = streamingOn(matrixKey, models.ServiceDisneyPlus)
			},
		},
		{
			name:   "service the user turned off",
			change: func(s *Selection) { s.User.Services[0].Active = false },
		},
		{
			name:   "user with no services",
			change: func(s *Selection) { s.User.Services = nil },
		},
		{
			name:   "event in another region",
			change: func(s *Selection) { s.Events[0].Region = "GB" },
		},
		{
			name:   "removal events alone",
			change: func(s *Selection) { s.Events = []models.AvailabilityEvent{removed(models.ServiceNetflix, day(-1))} },
		},
		{
			name:   "no stored title",
			change: func(s *Selection) { s.Titles = map[string]*models.Title{} },
		},
		{
			name:   "item not linked to tmdb",
			change: func(s *Selection) { s.Items[0].TMDbID = 0 },
		},
		{
			name: "two services at once are one entry, ordered by name",
			change: func(s *Selection) {
				s.Events = []models.AvailabilityEvent{added(models.ServiceNetflix, day(-1)), added(models.ServiceHulu, day(-2))}
				s.Titles[matrixKey] = streamingOn(matrixKey, models.ServiceNetflix, models.ServiceHulu)
			},
			want: []string{models.ServiceHulu, models.ServiceNetflix},
		},
		{
			name: "of two additions only the one still true is listed",
			change: func(s *Selection) {
				s.Events = []models.AvailabilityEvent{added(models.ServiceNetflix, day(-1)), added(models.ServiceHulu, day(-2))}
			},
			want: []string{models.ServiceNetflix},
		},
		{
			name: "reappearance soon after a removal is not news",
			change: func(s *Selection) {
				s.Events = []models.AvailabilityEvent{removed(models.ServiceNetflix, day(-3)), added(models.ServiceNetflix, day(-1))}
			},
		},
		{
			name: "return after a long absence is news",
			change: func(s *Selection) {
				s.Events = []models.AvailabilityEvent{removed(models.ServiceNetflix, day(-9)), added(models.ServiceNetflix, day(-1))}
			},
			want: []string{models.ServiceNetflix},
		},
		{
			name: "a removal on another service does not suppress",
			change: func(s *Selection) {
				s.Events = []models.AvailabilityEvent{removed(models.ServiceHulu, day(-2)), added(models.ServiceNetflix, day(-1))}
			},
			want: []string{models.ServiceNetflix},
		},
		{
			name: "a later removal does not suppress an earlier arrival that is true again",
			change: func(s *Selection) {
				s.Events = []models.AvailabilityEvent{added(models.ServiceNetflix, day(-2)), removed(models.ServiceNetflix, day(-1))}
			},
			want: []string{models.ServiceNetflix},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := base()
			tt.change(&sel)
			entries := Select(sel)

			if tt.want == nil {
				if len(entries) != 0 {
					t.Fatalf("Select() = %d entries (%v), want none", len(entries), entries[0].ServiceCodes)
				}
				return
			}
			if len(entries) != 1 {
				t.Fatalf("Select() = %d entries, want 1", len(entries))
			}
			if !reflect.DeepEqual(entries[0].ServiceCodes, tt.want) {
				t.Errorf("ServiceCodes = %v, want %v", entries[0].ServiceCodes, tt.want)
			}
		})
	}
}

func TestSelectEntryDetails(t *testing.T) {
	user := subscriber(models.ServiceMax, models.ServiceNetflix)
	zebra := &models.WatchlistItem{Title: "zebra", Type: models.WatchlistTypeShow, TMDbID: 2, AddedAt: day(-30)}
	alpha := &models.WatchlistItem{Title: "Alpha", Type: models.WatchlistTypeMovie, TMDbID: 1, Year: 2001, AddedAt: day(-30)}

	ev := func(key, code string) models.AvailabilityEvent {
		e := added(code, day(-1))
		e.TitleKey = key
		return e
	}
	entries := Select(Selection{
		User:   user,
		Region: "US",
		Items:  []*models.WatchlistItem{zebra, alpha},
		Titles: map[string]*models.Title{
			"tv:2":    streamingOn("tv:2", models.ServiceMax),
			"movie:1": streamingOn("movie:1", models.ServiceNetflix),
		},
		Events:      []models.AvailabilityEvent{ev("tv:2", models.ServiceMax), ev("movie:1", models.ServiceNetflix)},
		WindowStart: day(-7),
		Now:         now,
	})

	if len(entries) != 2 || entries[0].Title != "Alpha" || entries[1].Title != "zebra" {
		t.Fatalf("entries not sorted by title, ignoring case: %+v", entries)
	}
	if entries[0].Year != 2001 {
		t.Errorf("item year = %d, want the item's own 2001", entries[0].Year)
	}
	if entries[1].Year != 1999 {
		t.Errorf("year = %d, want the stored title's 1999 when the item has none", entries[1].Year)
	}
	if entries[1].PosterPath != "/poster.jpg" {
		t.Errorf("PosterPath = %q", entries[1].PosterPath)
	}
	if got, want := entries[1].ServiceNames(), []string{"HBO Max"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ServiceNames() = %v, want %v", got, want)
	}
}
