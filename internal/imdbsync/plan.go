// Package imdbsync keeps a watchlist in step with a list of IMDb title IDs.
// Two callers share it: the IMDb sync (the link request and the daily worker
// task) and the CSV import. Both hand it the IDs they read and get back what
// was added and, for the sync only, what was removed.
//
// The work is split so that the part that decides is easy to test:
//
//   - BuildPlan is a pure function. Given the user's items, the incoming IDs
//     and what those IDs resolve to on TMDb, it decides every write. All the
//     rules about what may be added and what may be removed live there.
//   - Reconciler.Run gathers BuildPlan's inputs (one query for the user's
//     items, then stored titles, then TMDb for whatever is still unknown) and
//     applies the plan with conditional writes, so a change the user makes
//     between the plan and a write always wins.
package imdbsync

import (
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MinMissingAge is how long an item must have been marked missing before the
// sync may remove it. The worker syncs a list about once a day, so the mark
// and the removal always come from two different fetches of the list.
const MinMissingAge = 12 * time.Hour

// Mode says who is reconciling, which decides what the plan may contain.
type Mode int

const (
	// ModeSync is the daily worker run. It adds, and it is the only mode
	// that marks items missing and removes them.
	ModeSync Mode = iota
	// ModeLink is the request that links a list. It adds and never removes,
	// and every missing mark is cleared before anything else is written, so
	// that both strikes against an item come from the list that is linked
	// now.
	ModeLink
	// ModeCSV is an uploaded IMDb export. It adds and never removes.
	ModeCSV
)

// String names the mode for logs.
func (m Mode) String() string {
	switch m {
	case ModeSync:
		return "sync"
	case ModeLink:
		return "link"
	case ModeCSV:
		return "csv"
	default:
		return "unknown"
	}
}

// source is the Source that items added in this mode carry.
func (m Mode) source() string {
	if m == ModeCSV {
		return models.SourceCSV
	}
	return models.SourceIMDb
}

// Target is the TMDb title an IMDb ID resolved to, with the little metadata
// a new watchlist item needs. It comes from TMDb, never from IMDb.
type Target struct {
	Ref   models.TitleRef
	Title string
	Year  int
}

// Outcome is what happened, or in a plan what will happen, to one incoming
// IMDb ID.
type Outcome int

const (
	// OutcomePending means the ID could not be resolved this time (TMDb
	// failed or time ran out) or its item could not be written. Nothing is
	// known about it; the next run tries again.
	OutcomePending Outcome = iota
	// OutcomeAdded means a new watchlist item was created for it.
	OutcomeAdded
	// OutcomeExisting means the user already has the title.
	OutcomeExisting
	// OutcomeUnmatched means TMDb knows no movie or show with this IMDb ID.
	OutcomeUnmatched
)

// Input is everything BuildPlan decides from.
type Input struct {
	Mode Mode
	// Now is the time of the run. A removal needs a mark at least
	// MinMissingAge older than it.
	Now time.Time
	// LinkedAt is when the list being synced was linked. It names the
	// generation of the link: a mark made before it is a strike on the
	// evidence of whatever was linked earlier, and counts for nothing.
	// ModeSync does not mark or remove without it.
	LinkedAt time.Time
	// Existing is every watchlist item the user has.
	Existing []*models.WatchlistItem
	// IDs are the incoming IMDb title IDs in list order. Repeats are allowed
	// and count once.
	IDs []string
	// Complete says IDs is the whole list: every page was read and the count
	// matched what IMDb reported. Only ModeSync looks at it.
	Complete bool
	// Resolved maps incoming IDs to TMDb titles, and Unmatched holds the ones
	// TMDb does not know. An ID in neither that the user does not already
	// have is pending.
	Resolved  map[string]Target
	Unmatched map[string]bool
}

// Addition is a watchlist item to create.
type Addition struct {
	IMDbID string
	Target Target
}

// Fill sets the IMDb ID of an item the user added by hand that turned out to
// be on the list. Nothing else about the item changes.
type Fill struct {
	ItemID primitive.ObjectID
	IMDbID string
}

// Removal deletes an item that has been missing from the list since
// MissingSince. The delete is conditional on that mark still being there and
// on the item not having been edited since the plan read it: UpdatedAt is
// the item's updated_at at that moment.
type Removal struct {
	ItemID       primitive.ObjectID
	MissingSince time.Time
	UpdatedAt    time.Time
}

// PruneDecision says whether a plan considered marking and removing items,
// and if not, why not.
type PruneDecision string

const (
	// PruneConsidered means the list was trusted as complete; items missing
	// from it were marked or, on the second strike, removed.
	PruneConsidered PruneDecision = "considered"
	// PruneSkippedMode means this mode never removes (link, CSV).
	PruneSkippedMode PruneDecision = "mode"
	// PruneSkippedNoLink means the run did not say which link it is for
	// (Input.LinkedAt), so a strike could not be tied to one.
	PruneSkippedNoLink PruneDecision = "no_link"
	// PruneSkippedIncomplete means the fetch did not cover the whole list.
	PruneSkippedIncomplete PruneDecision = "incomplete"
	// PruneSkippedEmpty means the list had no titles. An empty list is far
	// more likely to be a mistake or a fault than a wish to delete everything.
	PruneSkippedEmpty PruneDecision = "empty"
	// PruneSkippedPending means some IDs could not be resolved, so the plan
	// cannot tell whether one of them is an item under a new IMDb ID.
	PruneSkippedPending PruneDecision = "pending"
)

// Plan is every write a reconcile will make, and the counts that describe it.
type Plan struct {
	Mode Mode
	// Add is in list order.
	Add          []Addition
	Fill         []Fill
	ClearMissing []primitive.ObjectID
	MarkMissing  []primitive.ObjectID
	Remove       []Removal
	Prune        PruneDecision

	// Outcomes has one entry per distinct incoming ID.
	Outcomes map[string]Outcome
	// Total is the number of distinct incoming IDs; the four counts below
	// add up to it.
	Total     int
	Existing  int
	Unmatched int
	Pending   int
	// Repeats counts incoming IDs that had already appeared in the list.
	Repeats int
}

// BuildPlan decides what a reconcile does. It reads nothing but its input
// and has no side effects.
//
// Adding. An incoming ID is already on the watchlist when an item has that
// IMDb ID, or when the ID resolves to a title an item already points at (same
// type and TMDb ID). Such an item is left exactly as it is, whoever added it;
// the one exception is that an item without an IMDb ID gets the ID filled
// in. Every other resolved ID becomes a new item. IDs TMDb does not know are
// counted as unmatched and IDs that could not be resolved are pending;
// neither is an error.
//
// Removing happens in ModeSync only, and an item is removed only when all of
// these hold:
//
//  1. The sync added it, and the user has not touched it since: its status
//     is still "planned" and it has no tags (models.WatchlistItem.SyncOwned).
//  2. It is absent from a fetch that is Complete, of a list that is not
//     empty, with no ID left pending.
//  3. An earlier such fetch of the same link already marked it missing, at
//     least MinMissingAge ago. "The same link" means the mark is not older
//     than LinkedAt.
//
// The first fetch an item is missing from only marks it. Seeing it on the
// list again, in any fetch, clears the mark. A mark older than LinkedAt is
// stale: it was made for a list linked before this one. It is never grounds
// for removal; the plan clears it, and a later run may mark the item afresh.
// That makes the two strikes belong to one link by construction, whatever
// went wrong when the link changed.
func BuildPlan(in Input) Plan {
	plan := Plan{Mode: in.Mode, Outcomes: make(map[string]Outcome, len(in.IDs))}

	byIMDbID := make(map[string]*models.WatchlistItem, len(in.Existing))
	byTitle := make(map[string]*models.WatchlistItem, len(in.Existing))
	for _, item := range in.Existing {
		if item.IMDbID != "" {
			byIMDbID[item.IMDbID] = item
		}
		if ref, ok := item.TitleRef(); ok {
			byTitle[ref.Key()] = item
		}
	}

	// onList holds the items the incoming list accounts for.
	onList := make(map[primitive.ObjectID]bool, len(in.IDs))
	filled := make(map[primitive.ObjectID]bool)
	adding := make(map[string]bool)

	for _, id := range in.IDs {
		if _, seen := plan.Outcomes[id]; seen {
			plan.Repeats++
			continue
		}
		plan.Total++

		if item, ok := byIMDbID[id]; ok {
			onList[item.ID] = true
			plan.Outcomes[id] = OutcomeExisting
			plan.Existing++
			continue
		}

		target, resolved := in.Resolved[id]
		switch {
		case resolved && byTitle[target.Ref.Key()] != nil:
			// The user has the title under another IMDb ID or none at all,
			// which is how items added by hand are stored.
			item := byTitle[target.Ref.Key()]
			onList[item.ID] = true
			if item.IMDbID == "" && !filled[item.ID] {
				filled[item.ID] = true
				plan.Fill = append(plan.Fill, Fill{ItemID: item.ID, IMDbID: id})
			}
			plan.Outcomes[id] = OutcomeExisting
			plan.Existing++
		case resolved && adding[target.Ref.Key()]:
			// Two IMDb IDs for one TMDb title: the first one adds it.
			plan.Outcomes[id] = OutcomeExisting
			plan.Existing++
		case resolved:
			adding[target.Ref.Key()] = true
			plan.Add = append(plan.Add, Addition{IMDbID: id, Target: target})
			plan.Outcomes[id] = OutcomeAdded
		case in.Unmatched[id]:
			plan.Outcomes[id] = OutcomeUnmatched
			plan.Unmatched++
		default:
			plan.Outcomes[id] = OutcomePending
			plan.Pending++
		}
	}

	plan.Prune = pruneDecision(in, plan)
	if in.Mode == ModeCSV {
		// An uploaded file says nothing about the linked list.
		return plan
	}

	for _, item := range in.Existing {
		marked := item.IMDbMissingSince != nil
		stale := marked && item.IMDbMissingSince.Before(in.LinkedAt)
		switch {
		case marked && (in.Mode == ModeLink || stale || onList[item.ID] || !item.SyncOwned()):
			// Back on the list, or the user has made the item theirs, or the
			// link itself changed: the mark no longer means anything.
			plan.ClearMissing = append(plan.ClearMissing, item.ID)
		case plan.Prune != PruneConsidered || onList[item.ID] || !item.SyncOwned():
			// Not ours to remove, or no grounds to.
		case !marked:
			plan.MarkMissing = append(plan.MarkMissing, item.ID)
		case in.Now.Sub(*item.IMDbMissingSince) >= MinMissingAge:
			plan.Remove = append(plan.Remove, Removal{ItemID: item.ID, MissingSince: *item.IMDbMissingSince, UpdatedAt: item.UpdatedAt})
		}
	}
	return plan
}

// pruneDecision reports whether the incoming list is trustworthy enough to
// conclude that an item is no longer on it.
func pruneDecision(in Input, plan Plan) PruneDecision {
	switch {
	case in.Mode != ModeSync:
		return PruneSkippedMode
	case in.LinkedAt.IsZero():
		return PruneSkippedNoLink
	case !in.Complete:
		return PruneSkippedIncomplete
	case plan.Total == 0:
		return PruneSkippedEmpty
	case plan.Pending > 0:
		return PruneSkippedPending
	default:
		return PruneConsidered
	}
}
