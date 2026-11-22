package digest

import (
	"sort"
	"strings"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

// ReappearanceQuietPeriod suppresses "added" events for a service that
// dropped the same title only a short while earlier. Provider data flickers:
// a title can vanish from a service's listing for a day and come back. Without
// this guard an upstream blip across the catalog would be followed, one run
// later, by an email announcing every title a user already knew was there.
const ReappearanceQuietPeriod = 7 * 24 * time.Hour

// Entry is one title in a digest: a watchlist item and the services it newly
// became available on.
type Entry struct {
	Item       *models.WatchlistItem
	Title      string
	Year       int
	PosterPath string
	// ServiceCodes are the user's services that newly stream the title,
	// ordered by display name.
	ServiceCodes []string
	// EventIDs are the availability events the entry announces, sorted. Once
	// the digest is sent they are what the delivery log records.
	EventIDs []string
}

// ServiceNames returns the display names of the entry's services.
func (e Entry) ServiceNames() []string {
	names := make([]string, len(e.ServiceCodes))
	for i, code := range e.ServiceCodes {
		names[i] = models.ServiceName(code)
	}
	return names
}

// Selection is everything Select needs to decide a user's digest. It is plain
// data so the rules can be tested without a database.
type Selection struct {
	User   *models.User
	Region string
	Items  []*models.WatchlistItem
	// Titles holds the stored titles for Items, keyed by title key.
	Titles map[string]*models.Title
	// Events are the availability events for those titles. They should reach
	// back ReappearanceQuietPeriod before WindowStart so that reappearances
	// can be recognised; order does not matter.
	Events []models.AvailabilityEvent
	// Delivered holds the IDs of events this user was already sent. The window
	// overlaps the previous digest's, so this is what stops a repeat.
	Delivered map[string]bool
	// WindowStart and Now bound the detections this digest covers:
	// WindowStart < detected_at <= Now.
	WindowStart time.Time
	Now         time.Time
}

// Select returns the titles worth emailing the user about, one entry per
// title, sorted by title. A title qualifies through an "added" event that:
//
//   - is in the user's region and for a service the user has active,
//   - was detected inside the window,
//   - has not been sent to this user before,
//   - was detected after the user put the title on their list (a title added
//     while it was already streaming is not news),
//   - is still true: the stored snapshot shows the title on that service now,
//   - is not a reappearance within ReappearanceQuietPeriod of a removal,
//
// and only if the item is not marked finished.
func Select(sel Selection) []Entry {
	active := sel.User.ActiveServiceCodes()
	if len(active) == 0 {
		return nil
	}

	type serviceKey struct{ titleKey, code string }
	added := make(map[string][]models.AvailabilityEvent)
	removals := make(map[serviceKey][]time.Time)
	for _, ev := range sel.Events {
		if ev.Region != sel.Region {
			continue
		}
		switch ev.Change {
		case models.ChangeAdded:
			added[ev.TitleKey] = append(added[ev.TitleKey], ev)
		case models.ChangeRemoved:
			k := serviceKey{ev.TitleKey, ev.ServiceCode}
			removals[k] = append(removals[k], ev.DetectedAt)
		}
	}

	reappeared := func(ev models.AvailabilityEvent) bool {
		for _, removedAt := range removals[serviceKey{ev.TitleKey, ev.ServiceCode}] {
			if removedAt.Before(ev.DetectedAt) && ev.DetectedAt.Sub(removedAt) <= ReappearanceQuietPeriod {
				return true
			}
		}
		return false
	}

	var entries []Entry
	for _, item := range sel.Items {
		if item.Status == models.WatchlistStatusFinished {
			continue
		}
		ref, ok := item.TitleRef()
		if !ok {
			continue
		}
		title := sel.Titles[ref.Key()]
		snapshot, ok := title.Snapshot(sel.Region)
		if !ok {
			continue
		}

		codes := make(map[string]bool)
		var eventIDs []string
		for _, ev := range added[ref.Key()] {
			switch {
			case !active[ev.ServiceCode]:
			case !ev.DetectedAt.After(sel.WindowStart) || ev.DetectedAt.After(sel.Now):
			case sel.Delivered[ev.ID]:
			case !ev.DetectedAt.After(item.AddedAt):
			case !snapshot.HasService(ev.ServiceCode):
			case reappeared(ev):
			default:
				codes[ev.ServiceCode] = true
				eventIDs = append(eventIDs, ev.ID)
			}
		}
		if len(codes) == 0 {
			continue
		}

		sort.Strings(eventIDs)
		entry := Entry{
			Item:       item,
			Title:      item.Title,
			Year:       item.Year,
			PosterPath: title.PosterPath,
			EventIDs:   eventIDs,
		}
		if entry.Year == 0 {
			entry.Year = title.Year
		}
		for code := range codes {
			entry.ServiceCodes = append(entry.ServiceCodes, code)
		}
		sort.Slice(entry.ServiceCodes, func(i, j int) bool {
			return models.ServiceName(entry.ServiceCodes[i]) < models.ServiceName(entry.ServiceCodes[j])
		})
		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Title) < strings.ToLower(entries[j].Title)
	})
	return entries
}
