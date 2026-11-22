package server

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/imdbsync"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/imdb"
)

const (
	// The server cuts a response off 30 s after the request was read
	// (WriteTimeout in server.go). An import has until importDeadline, 25 s
	// from its start, and the reconcile (reading the watchlist, asking TMDb,
	// writing the rows) must be over by importReconcileDeadline, 22 s, which
	// leaves time to build the answer and write it.

	// importDeadline is when the handler stops doing optional work
	// (fetching availability for the new titles).
	importDeadline = 25 * time.Second
	// importReconcileDeadline is when the reconcile must be over, counted
	// from the start of the request. It is a deadline on the reconcile's
	// context, so it holds for time spent waiting on the database as much as
	// for time spent waiting on TMDb. Rows that did not make it are
	// reported; uploading the file again continues where it stopped.
	importReconcileDeadline = 22 * time.Second
	// importLookupBudget is how long an import may spend asking TMDb about
	// titles nobody has listed before. Rows that do not fit are reported;
	// uploading the file again continues where it stopped, because rows
	// that made it are then duplicates and cost no lookup.
	importLookupBudget = 16 * time.Second
	// importWriteReserve is kept back from the TMDb lookups for the writes
	// that follow them.
	importWriteReserve = 4 * time.Second
	// importEnsureBudget bounds fetching availability after the rows are
	// in, however many there are.
	importEnsureBudget = 6 * time.Second
	// maxImportSize is the largest file accepted.
	maxImportSize = int64(10 << 20)
	// maxImportTitles is the most titles one file may bring in; an IMDb
	// list holds no more than that.
	maxImportTitles = imdb.MaxListSize
	// maxRetryRowErrors is the most rows reported one by one as "try
	// again". Past it, they are summed up in a single entry.
	maxRetryRowErrors = 20
)

// importWatchlistHandler godoc
// @Summary Import watchlist from CSV
// @Description Bulk import watchlist items from an IMDb CSV export (any CSV with a "Const" column of IMDb title IDs). Rows the user already has are counted as duplicates and cost nothing. New titles are added as planned items with source "csv". An import only ever adds; it never removes or changes an item. When TMDb is slow, the rows that could not be looked up in time are reported in "errors"; uploading the same file again continues where it stopped.
// @Tags Watchlist
// @Security BearerAuth
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "CSV file containing watchlist items (max 10MB)"
// @Success 200 {object} WatchlistImportResponse "Import results with counts"
// @Failure 400 {object} ErrorResponse "Invalid file or format, or no row could be imported"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 500 {object} ErrorResponse "Import failed"
// @Failure 503 {object} ErrorResponse "Import service unavailable"
// @Router /api/watchlist/import [post]
func (s *Server) importWatchlistHandler(c *gin.Context) {
	start := time.Now()
	// One deadline for the database from the very first read to the end of
	// the reconcile. Hitting it during the reconcile ends the same way as
	// TMDb being too slow: the rows that were written are reported as
	// imported, the others as "upload again".
	deadline := importReconcileDeadline
	if s.reconcileDeadline > 0 {
		deadline = s.reconcileDeadline
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), start.Add(deadline))
	defer cancel()

	user, ok := s.getUserWithin(ctx, c)
	if !ok {
		return
	}
	if !s.reconciler.CanResolve() {
		jsonError(c, http.StatusServiceUnavailable, "import unavailable")
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		jsonError(c, http.StatusBadRequest, "file is required")
		return
	}
	if fileHeader.Size == 0 {
		jsonError(c, http.StatusBadRequest, "file is empty")
		return
	}
	if fileHeader.Size > maxImportSize {
		jsonError(c, http.StatusBadRequest, "file too large (max 10MB)")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		jsonError(c, http.StatusBadRequest, "failed to open file")
		return
	}
	defer func() { _ = file.Close() }()

	// Every row is read before anything is looked up, so that the user's
	// existing items can be loaded once and the rows they already cover
	// never reach TMDb.
	rows, rowErrors, err := parseIMDbCSV(file)
	if err != nil {
		jsonError(c, http.StatusBadRequest, err.Error())
		return
	}

	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.imdbID
	}
	budget := s.resolveBudget
	if budget <= 0 {
		budget = max(min(importLookupBudget, deadline-importWriteReserve-time.Since(start)), time.Second)
	}
	res, err := s.reconciler.Run(ctx, imdbsync.Request{
		UserID:       user.ID,
		Mode:         imdbsync.ModeCSV, // adds only, with source "csv"
		IDs:          ids,
		LookupBudget: budget,
	})
	if err != nil {
		log.Printf("watchlist import: reconcile stopped early: %v", err)
		if res.Outcomes == nil {
			jsonError(c, http.StatusInternalServerError, "import failed")
			return
		}
	}

	result := importResult(rows, rowErrors, res)
	if result.Imported == 0 && result.Duplicates == 0 && len(result.Errors) > 0 {
		jsonError(c, http.StatusBadRequest, "no rows imported")
		return
	}

	// Availability for the new titles is fetched once, after the rows are in,
	// with bounded concurrency and a fixed budget: the import does not get
	// slower per row. Whatever does not fit is picked up by the list's miss
	// handling or the daily refresh.
	s.ensureTitles(c.Request.Context(), user, res.AddedTitles, min(importEnsureBudget, importDeadline-time.Since(start)))

	c.JSON(http.StatusOK, result)
}

// WatchlistImportResponse is the body of POST /api/watchlist/import.
type WatchlistImportResponse struct {
	// Imported counts rows that became new watchlist items.
	Imported int `json:"imported" example:"12"`
	// Duplicates counts rows the user already had, or that repeat an
	// earlier row of the file.
	Duplicates int `json:"duplicates" example:"230"`
	// Errors lists rows that were not imported, in file order.
	Errors []WatchlistRowError `json:"errors"`
}

// WatchlistRowError says why one row of an import was not imported.
type WatchlistRowError struct {
	// Row is the line of the file, counting the header as row 1.
	Row    int    `json:"row" example:"7"`
	Reason string `json:"reason" example:"no tmdb match for imdb id tt0000001"`
}

// csvRow is one data row of an import that carries a usable IMDb title ID.
type csvRow struct {
	number int // line of the file; the header is 1
	imdbID string
}

// parseIMDbCSV reads an IMDb export: a header row with a "Const" column,
// then one title per row. It returns the rows that carry a valid IMDb title
// ID and an error entry for every row that does not. The error result is for
// a file that is not such a CSV at all.
func parseIMDbCSV(reader io.Reader) ([]csvRow, []WatchlistRowError, error) {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	csvReader.TrimLeadingSpace = true
	csvReader.LazyQuotes = true

	header, err := csvReader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil, errors.New("file is empty")
		}
		return nil, nil, errors.New("file is not a CSV")
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	constIdx := -1
	for i, col := range header {
		if strings.TrimSpace(col) == "Const" {
			constIdx = i
			break
		}
	}
	if constIdx < 0 {
		return nil, nil, errors.New("missing required column: Const")
	}

	var (
		rows     []csvRow
		rowErrs  []WatchlistRowError
		distinct = make(map[string]bool)
		number   = 1
		overflow = 0
	)
	for {
		record, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		number++
		if err != nil {
			rowErrs = append(rowErrs, WatchlistRowError{Row: number, Reason: "failed to parse row"})
			continue
		}
		id := ""
		if len(record) > constIdx {
			id = strings.TrimSpace(record[constIdx])
		}
		switch {
		case id == "":
			rowErrs = append(rowErrs, WatchlistRowError{Row: number, Reason: "missing IMDb ID"})
		case !imdb.ValidTitleID(id):
			rowErrs = append(rowErrs, WatchlistRowError{Row: number, Reason: "not an IMDb title ID (tt…)"})
		case !distinct[id] && len(distinct) >= maxImportTitles:
			if overflow == 0 {
				rowErrs = append(rowErrs, WatchlistRowError{Row: number, Reason: fmt.Sprintf("only the first %d titles of a file are imported; this row and the ones after it were skipped", maxImportTitles)})
			}
			overflow++
		default:
			distinct[id] = true
			rows = append(rows, csvRow{number: number, imdbID: id})
		}
	}
	return rows, rowErrs, nil
}

// importResult turns what the reconcile did with each IMDb ID into the
// per-row answer the client shows. rowErrors are the rows that never had a
// usable ID; the result lists every error in file order.
func importResult(rows []csvRow, rowErrors []WatchlistRowError, res imdbsync.Result) WatchlistImportResponse {
	// Errors is always an array in the response, never null.
	out := WatchlistImportResponse{Errors: append([]WatchlistRowError{}, rowErrors...)}
	seen := make(map[string]bool, len(rows))
	var retry []csvRow
	for _, row := range rows {
		repeat := seen[row.imdbID]
		seen[row.imdbID] = true
		switch res.Outcomes[row.imdbID] {
		case imdbsync.OutcomeAdded:
			if repeat {
				out.Duplicates++
			} else {
				out.Imported++
			}
		case imdbsync.OutcomeExisting:
			out.Duplicates++
		case imdbsync.OutcomeUnmatched:
			out.Errors = append(out.Errors, WatchlistRowError{Row: row.number, Reason: "no tmdb match for imdb id " + row.imdbID})
		default:
			if !repeat {
				retry = append(retry, row)
			}
		}
	}

	// Rows that could not be looked up or saved this time. A slow TMDb can
	// leave thousands of them, which is one message, not thousands.
	if len(retry) > maxRetryRowErrors {
		out.Errors = append(out.Errors, WatchlistRowError{
			Row:    retry[0].number,
			Reason: fmt.Sprintf("import ran out of time with %d titles left, from this row on; upload the file again to continue", len(retry)),
		})
	} else {
		for _, row := range retry {
			out.Errors = append(out.Errors, WatchlistRowError{Row: row.number, Reason: "could not be imported this time; upload the file again to retry"})
		}
	}
	sort.SliceStable(out.Errors, func(i, j int) bool { return out.Errors[i].Row < out.Errors[j].Row })
	return out
}
