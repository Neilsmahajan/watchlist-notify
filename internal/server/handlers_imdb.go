package server

import (
	"context"
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/imdbsync"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/imdb"
)

// Linking an IMDb list is one request that fetches the list and adds its
// titles before it answers. These bound it so the answer arrives well inside
// the server's 30 s write timeout; whatever does not fit is finished by the
// worker's next daily sync.
const (
	// The server cuts a response off 30 s after the request was read
	// (WriteTimeout in server.go), so everything a link request does has a
	// place in that time:
	//
	//	 0 s .. 20 s  claim, fetch from IMDb, reconcile: reads, TMDb lookups
	//	              and writes, all under one deadline (imdbLinkDeadline)
	//	   .. 25 s  storing the link, on its own clock (imdbLinkStoreTimeout)
	//	              so that it still happens when the deadline was hit
	//	   .. 30 s  left for the answer
	//
	// Availability for the new titles is fetched only with what is left of
	// the first 20 s.

	// imdbLinkDeadline is when the claim, the fetch and the reconcile must
	// be over, counted from the start of the request. It is a deadline on
	// their context, so it holds for time spent waiting on the database as
	// much as for time spent waiting on IMDb or TMDb.
	imdbLinkDeadline = 20 * time.Second
	// imdbLinkFetchBudget bounds reading the list from IMDb.
	imdbLinkFetchBudget = 8 * time.Second
	// imdbLinkWriteReserve is kept back from the TMDb lookups for the
	// writes that follow them.
	imdbLinkWriteReserve = 5 * time.Second
	// imdbLinkStoreTimeout bounds storing the link after the reconcile.
	imdbLinkStoreTimeout = 5 * time.Second
	// imdbLinkEnsureBudget bounds fetching availability for the new titles.
	imdbLinkEnsureBudget = 3 * time.Second
	// imdbLinkMinGap is the least time between a fetch of a user's list,
	// by a link request or by the worker, and that user's next link request
	// or unlink. It does two jobs. Every link attempt is a fetch from IMDb,
	// and IMDb's terms are the reason this service fetches as little as it
	// can (see package imdb). And it is the lockout that lets a worker run
	// act on the link it read: for this long after the worker's claim the
	// link cannot change (see imdbsync.OwnershipLease).
	imdbLinkMinGap = imdbsync.LinkLockout
)

// imdbListFetcher reads a public IMDb list; *imdb.Client does.
type imdbListFetcher interface {
	FetchList(ctx context.Context, sourceID string) (imdb.List, error)
}

// IMDbLinkRequest is the body of PUT /api/me/imdb.
type IMDbLinkRequest struct {
	// Source is an IMDb user ID (ur…), a list ID (ls…), or an IMDb URL
	// containing one.
	Source string `json:"source" example:"https://www.imdb.com/user/ur12345678/watchlist"`
}

// IMDbLinkResponse is the body of a successful PUT /api/me/imdb.
type IMDbLinkResponse struct {
	IMDbSync models.IMDbSync `json:"imdb_sync"`
	Result   IMDbLinkResult  `json:"result"`
}

// IMDbLinkResult says what the first sync of a newly linked list did.
type IMDbLinkResult struct {
	// Total is the number of titles on the IMDb list.
	Total int `json:"total" example:"242"`
	// Added is how many of them became new watchlist items.
	Added int `json:"added" example:"230"`
	// Existing is how many were already on the watchlist.
	Existing int `json:"existing" example:"10"`
	// Unmatched is how many TMDb does not know, so they cannot be tracked.
	Unmatched int `json:"unmatched" example:"2"`
	// Complete is false when the list was only partly read or some titles
	// are still to be looked up; the daily sync finishes the job.
	Complete bool `json:"complete" example:"true"`
}

// linkIMDbHandler godoc
// @Summary Link a public IMDb list
// @Description Links the user's public IMDb Watchlist (by user ID, ur…) or any public IMDb list (ls…), adds its titles to the watchlist now, and keeps the watchlist in step with it once a day from then on. Linking again replaces the link. Titles are added as planned items with source "imdb". Linking itself leaves every item the user already has exactly as it is. The daily sync, from its first run on, adopts items the user imported from IMDb earlier: an item that carries the IMDb ID of a title on the list (which only an import stores), is still "planned" and has no tags gets source "imdb" and follows the list from then on. Items added by hand are never adopted. The daily sync removes an item only if its source is "imdb", the user has not changed its status or tagged it, and it has been missing from two complete fetches of the list at least 12 hours apart. IMDb does not permit public or commercial use of its data, so this is strictly opt-in, stores only IMDb title IDs (everything else comes from TMDb), and reads a list at most once a day: there is no "sync now", and a second link attempt within 60 seconds is refused.
// @Tags Users
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body IMDbLinkRequest true "The IMDb user ID, list ID or URL to link"
// @Success 200 {object} IMDbLinkResponse "The stored link and what the first sync did"
// @Failure 400 {object} ErrorResponse "Not an IMDb user ID, list ID or URL; or the list is too large"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 404 {object} ErrorResponse "IMDb has no such list, or it is not public"
// @Failure 429 {object} ErrorResponse "A link was attempted less than 60 seconds ago"
// @Header 429 {integer} Retry-After "Seconds to wait before trying again"
// @Failure 500 {object} ErrorResponse "The link could not be stored"
// @Failure 502 {object} ErrorResponse "IMDb could not be reached"
// @Failure 503 {object} ErrorResponse "IMDb sync is not configured on this server"
// @Router /api/me/imdb [put]
func (s *Server) linkIMDbHandler(c *gin.Context) {
	start := time.Now()
	// One deadline for everything up to the end of the reconcile, database
	// time included, from the very first read. A database that stops
	// answering then ends in the same way as a TMDb that is too slow: what
	// was written stays, the rest is counted as not done yet, and the
	// request still answers in time.
	deadline := imdbLinkDeadline
	if s.reconcileDeadline > 0 {
		deadline = s.reconcileDeadline
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), start.Add(deadline))
	defer cancel()

	user, ok := s.getUserWithin(ctx, c)
	if !ok {
		return
	}

	var body IMDbLinkRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonError(c, http.StatusBadRequest, "invalid body")
		return
	}
	sourceID, err := imdb.ParseSource(body.Source)
	if err != nil {
		jsonError(c, http.StatusBadRequest, "Enter an IMDb user ID (ur…), a list ID (ls…) or the link to an IMDb watchlist or list.")
		return
	}
	if s.imdbLists == nil || !s.reconciler.CanResolve() {
		jsonError(c, http.StatusServiceUnavailable, "IMDb sync is unavailable")
		return
	}

	// Claiming the attempt is atomic, so two requests racing each other
	// cannot both reach IMDb. It also makes this fetch the user's fetch of
	// the day: the worker will not read the list again for 20 hours.
	claimed, err := s.db.ClaimIMDbAttempt(ctx, user.ID, start, imdbLinkMinGap)
	if err != nil {
		log.Printf("imdb link: claim failed: %v", err)
		jsonError(c, http.StatusInternalServerError, "failed to link IMDb list")
		return
	}
	if !claimed {
		c.Header("Retry-After", strconv.Itoa(int(imdbLinkMinGap.Seconds())))
		jsonError(c, http.StatusTooManyRequests, "An IMDb list was linked a moment ago. Try again in a minute.")
		return
	}

	fetchCtx, cancelFetch := context.WithTimeout(ctx, imdbLinkFetchBudget)
	list, fetchErr := s.imdbLists.FetchList(fetchCtx, sourceID)
	cancelFetch()
	switch {
	case errors.Is(fetchErr, imdb.ErrNotFound):
		jsonError(c, http.StatusNotFound, "IMDb has no public list with that ID. Make the list public on IMDb (in the list's privacy settings), then try again.")
		return
	case errors.Is(fetchErr, imdb.ErrTooLarge):
		jsonError(c, http.StatusBadRequest, "That list is too large to sync (more than "+strconv.Itoa(imdb.MaxListSize)+" titles).")
		return
	case fetchErr != nil && len(list.Entries) == 0:
		// The client's errors carry a status or an error code, never a URL.
		log.Printf("imdb link: fetch failed: %v", fetchErr)
		jsonError(c, http.StatusBadGateway, "IMDb could not be reached. Try again in a few minutes.")
		return
	}
	// A list read only in part (a later page failed) is still linked: the
	// titles that were read are added, the status says "partial", and the
	// daily sync reads the whole list.

	lookupBudget := s.resolveBudget
	if lookupBudget <= 0 {
		lookupBudget = max(deadline-imdbLinkWriteReserve-time.Since(start), time.Second)
	}
	out := s.reconciler.SyncList(ctx, imdbsync.SyncRequest{
		UserID:       user.ID,
		Previous:     user.IMDbSync,
		SourceID:     sourceID,
		Mode:         imdbsync.ModeLink, // adds only; removing is the worker's job
		At:           start,             // the claim; see SyncRequest.At
		List:         list,
		FetchErr:     fetchErr,
		LookupBudget: lookupBudget,
	})
	if out.Err != nil {
		log.Printf("imdb link: reconcile stopped early: %v", out.Err)
		if errors.Is(out.Err, imdbsync.ErrMarksNotCleared) || out.Result.Outcomes == nil {
			// Either the strikes made for the list linked before are still
			// in place, or nothing was read or written. Do not link on the
			// strength of that; the link that was there stays as it was.
			jsonError(c, http.StatusInternalServerError, "failed to link IMDb list")
			return
		}
	}

	// The titles are in. Store the link even if the client has gone away or
	// the deadline was hit, or they would sit there with nothing keeping
	// them in step: a reconcile that ran out of time is stored as "partial"
	// and the daily sync finishes it.
	linkCtx, cancelLink := context.WithTimeout(context.WithoutCancel(c.Request.Context()), imdbLinkStoreTimeout)
	err = s.db.LinkIMDb(linkCtx, user.ID, out.State)
	cancelLink()
	if err != nil {
		log.Printf("imdb link: storing the link failed: %v", err)
		jsonError(c, http.StatusInternalServerError, "failed to link IMDb list")
		return
	}

	// Availability for the new titles, once, with what is left of the time.
	s.ensureTitles(ctx, user, out.Result.AddedTitles, min(imdbLinkEnsureBudget, deadline-time.Since(start)))

	c.JSON(http.StatusOK, IMDbLinkResponse{
		IMDbSync: out.State,
		Result: IMDbLinkResult{
			Total:     out.State.Total,
			Added:     out.Result.Added,
			Existing:  out.Result.Existing,
			Unmatched: out.Result.Unmatched,
			Complete:  out.State.LastStatus == models.IMDbStatusOK,
		},
	})
}

// unlinkIMDbHandler godoc
// @Summary Unlink the IMDb list
// @Description Stops syncing with the linked IMDb list. Nothing is removed from the watchlist: items the sync added or adopted stay, and are never removed automatically afterwards. Every "missing from the list" mark (imdb_missing_since) on the user's items is cleared. Unlinking when nothing is linked also succeeds. Within 60 seconds of a fetch of the user's list, by a link request or by the daily sync, the answer is 429 with Retry-After and nothing is changed: the list is still linked; send the request again after that many seconds. On a 500 the list is still linked too.
// @Tags Users
// @Security BearerAuth
// @Success 204 "Unlinked"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 429 {object} ErrorResponse "The list was fetched less than a minute ago; still linked"
// @Header 429 {integer} Retry-After "Seconds to wait before trying again"
// @Failure 500 {object} ErrorResponse "The link could not be removed"
// @Router /api/me/imdb [delete]
func (s *Server) unlinkIMDbHandler(c *gin.Context) {
	start := time.Now()
	user, ok := s.getUser(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	// A sync run that claimed its fetch less than a minute ago may be
	// adopting, marking and removing items for this link right now, and for
	// that long it relies on the link staying as it is (see
	// imdbsync.OwnershipLease). So does a link request that is still under
	// way. Unlinking waits for them.
	locked := func(wait time.Duration) {
		c.Header("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
		jsonError(c, http.StatusTooManyRequests, "The IMDb list was read a moment ago. Try again in a minute.")
	}
	if at := user.IMDbAttemptAt; at != nil && start.Sub(*at) < imdbLinkMinGap {
		locked(imdbLinkMinGap - max(0, start.Sub(*at)))
		return
	}
	// The strikes go before the link: an account without a link carries no
	// strike that a later link could inherit. If they cannot be cleared the
	// list stays linked and the user tries again.
	if _, err := s.db.ClearIMDbMissingMarks(ctx, user.ID); err != nil {
		log.Printf("imdb unlink: clearing missing marks failed: %v", err)
		jsonError(c, http.StatusInternalServerError, "failed to unlink IMDb list")
		return
	}
	// The check above was a read; a run can have claimed its fetch since.
	// The write makes the same check itself, atomically, and that is the one
	// the lease rests on. When it refuses, the strikes cleared a moment ago
	// are the only thing this request changed, and losing a strike only
	// ever delays a removal.
	unlinked, err := s.db.UnlinkIMDb(ctx, user.ID, start, imdbLinkMinGap)
	if err != nil {
		log.Printf("imdb unlink failed: %v", err)
		jsonError(c, http.StatusInternalServerError, "failed to unlink IMDb list")
		return
	}
	if !unlinked {
		locked(imdbLinkMinGap)
		return
	}
	c.Status(http.StatusNoContent)
}
