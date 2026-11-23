package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/cache"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/middleware"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

// The tools the MCP endpoint offers. Each is a thin layer over what the REST
// API already does: it turns the arguments into a call of the same function
// the matching REST handler calls, and the result into a small answer a model
// can read. None of them queries the database on its own terms, and every
// query they cause is filtered by the ID of the user the token belongs to.

const (
	mcpServerName    = "watchlist-notify"
	mcpServerVersion = "1.0.0"

	mcpListDefaultLimit = 25
	mcpListMaxLimit     = 100
	// TMDb serves twenty results a page; one page is all a search reads.
	mcpSearchDefaultLimit = 10
	mcpSearchMaxLimit     = 20
	mcpMaxQueryLen        = 100
	mcpMaxTags            = 20
	mcpMaxTagLen          = 50
	// mcpOverviewLen cuts a synopsis down to roughly a line.
	mcpOverviewLen = 200
)

const mcpInstructions = `Watchlist Notify keeps one person's watchlist of movies and TV shows and knows which of their streaming services each title is on right now.

To add a title, find it with search_titles and pass its tmdb_id and type to add_to_watchlist. To change or remove one, take its id from list_watchlist. Streaming availability is for the user's own region and services, which they choose in the app.`

// mcpToolSet collects the tools while the server is being built.
type mcpToolSet struct {
	api    *Server
	server *mcp.Server
	// scopes maps each tool's name to the scope a token needs to call it.
	// The HTTP layer reads it to answer 403 before a tool runs.
	scopes map[string]string
}

// newMCPServer returns the MCP server with every tool registered, and the
// scope each tool needs.
func (s *Server) newMCPServer() (*mcp.Server, map[string]string) {
	server := mcp.NewServer(
		&mcp.Implementation{Name: mcpServerName, Title: "Watchlist Notify", Version: mcpServerVersion},
		&mcp.ServerOptions{
			Instructions: mcpInstructions,
			// Tools, and nothing that needs a connection kept open: the list
			// of tools never changes while the process runs, so there are no
			// change notifications to subscribe to.
			Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
		},
	)
	set := &mcpToolSet{api: s, server: server, scopes: make(map[string]string)}

	addMCPTool(set, &mcp.Tool{
		Name:  "list_watchlist",
		Title: "List watchlist",
		Description: "List the movies and TV shows on the user's watchlist, newest first, with the user's own streaming services that each one is on right now. " +
			"Filter by status, type, part of the title, or to only what is streaming on the user's services. " +
			"Results are paged: when next_offset is present, pass it as offset to get the next page.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPointer(false)},
	}, middleware.ScopeWatchlistRead, func(p map[string]*jsonschema.Schema) {
		p["status"].Enum = statusEnum
		p["type"].Enum = typeEnum
		p["query"].MaxLength = intPointer(mcpMaxQueryLen)
		bounded(p["limit"], 1, mcpListMaxLimit, mcpListDefaultLimit)
		bounded(p["offset"], 0, 1_000_000, 0)
	}, s.mcpListWatchlist)

	addMCPTool(set, &mcp.Tool{
		Name:  "whats_streaming",
		Title: "What's streaming",
		Description: "Answer \"what can I watch right now?\" in one call: the titles on the user's watchlist that are streaming on the user's own services, grouped by service. " +
			"A title on two services is listed under both. Optionally only movies or only shows.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPointer(false)},
	}, middleware.ScopeWatchlistRead, func(p map[string]*jsonschema.Schema) {
		p["type"].Enum = typeEnum
	}, s.mcpWhatsStreaming)

	addMCPTool(set, &mcp.Tool{
		Name:  "search_titles",
		Title: "Search titles",
		Description: "Search The Movie Database (TMDb) for movies and TV shows by name. Use it to find the tmdb_id and type that add_to_watchlist needs. " +
			"It searches the whole catalog, not the user's watchlist; for that, use list_watchlist with a query.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPointer(true)},
	}, middleware.ScopeWatchlistRead, func(p map[string]*jsonschema.Schema) {
		p["query"].MinLength = intPointer(1)
		p["query"].MaxLength = intPointer(mcpMaxQueryLen)
		p["type"].Enum = typeEnum
		bounded(p["limit"], 1, mcpSearchMaxLimit, mcpSearchDefaultLimit)
	}, s.mcpSearchTitles)

	addMCPTool(set, &mcp.Tool{
		Name:  "add_to_watchlist",
		Title: "Add to watchlist",
		Description: "Add a movie or TV show to the user's watchlist, by the tmdb_id and type that search_titles returned. The item starts as planned. " +
			"Adding a title that is already there changes nothing and returns the existing item with already_on_watchlist set to true.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(false), IdempotentHint: true, OpenWorldHint: boolPointer(true)},
	}, middleware.ScopeWatchlistWrite, func(p map[string]*jsonschema.Schema) {
		p["tmdb_id"].Minimum = floatPointer(1)
		p["type"].Enum = typeEnum
	}, s.mcpAddToWatchlist)

	addMCPTool(set, &mcp.Tool{
		Name:  "update_watchlist_item",
		Title: "Update watchlist item",
		Description: "Change the status of an item on the user's watchlist, its tags, or both. Take the id from list_watchlist. " +
			"A field that is left out keeps its value. tags replaces the whole list of tags; pass an empty list to remove them all.",
		// It overwrites two fields the user can set back; nothing is deleted.
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(false), IdempotentHint: true, OpenWorldHint: boolPointer(false)},
	}, middleware.ScopeWatchlistWrite, func(p map[string]*jsonschema.Schema) {
		p["status"].Enum = statusEnum
		p["tags"].MaxItems = intPointer(mcpMaxTags)
		p["tags"].Items.MaxLength = intPointer(mcpMaxTagLen)
	}, s.mcpUpdateWatchlistItem)

	addMCPTool(set, &mcp.Tool{
		Name:  "remove_from_watchlist",
		Title: "Remove from watchlist",
		Description: "Permanently remove an item from the user's watchlist, with its status and tags. Take the id from list_watchlist. " +
			"This cannot be undone; the title can only be added again as a new item.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true), IdempotentHint: true, OpenWorldHint: boolPointer(false)},
	}, middleware.ScopeWatchlistWrite, nil, s.mcpRemoveFromWatchlist)

	return server, set.scopes
}

// toolError is a failure whose message is safe, and useful, to show to the
// client. cause, when there is one, is logged and not shown.
type toolError struct {
	message string
	cause   error
}

func (e *toolError) Error() string { return e.message }

// addMCPTool registers a tool. run receives the user the token belongs to and
// the validated arguments, and returns the structured result with a one-line
// summary of it.
//
// Everything the tools have in common happens here:
//
//   - The schemas are derived from In and Out; describe adjusts the input
//     properties with what Go types cannot say (enums, bounds, defaults). The
//     SDK validates arguments against the schema before run is called.
//   - The scope is checked. The HTTP layer has normally refused the request
//     already, with the 403 that lets a client ask for more; this check is
//     the one that cannot be bypassed, because it sits next to the code it
//     protects and reads the token the transport attached to this very call.
//   - The token's identity becomes a user through resolveUser, the same
//     lookup the REST API uses.
//   - The call gets a deadline.
//   - An error becomes a tool error with a message meant for the model, and
//     anything unexpected is logged and replaced with a generic one.
//   - The result is returned as structured content and, for clients that do
//     not read that, as a summary followed by the same JSON as text.
func addMCPTool[In, Out any](
	set *mcpToolSet,
	tool *mcp.Tool,
	scope string,
	describe func(properties map[string]*jsonschema.Schema),
	run func(ctx context.Context, user *models.User, in In) (out Out, summary string, err error),
) {
	tool.InputSchema = schemaOf[In](describe)
	tool.OutputSchema = schemaOf[Out](nil)
	set.scopes[tool.Name] = scope
	api, name := set.api, tool.Name

	mcp.AddTool(set.server, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var none Out
		fail := func(message string) (*mcp.CallToolResult, Out, error) {
			return nil, none, errors.New(message)
		}

		// What the transport attached to this call: the verified token.
		token := req.Extra
		if token == nil || !middleware.HasScope(token.TokenInfo, scope) {
			return fail(fmt.Sprintf("This needs the %s scope, which the user has not granted. Ask them to reconnect Watchlist Notify and allow it.", scope))
		}
		identity, ok := middleware.MCPIdentityFrom(token.TokenInfo)
		if !ok {
			return fail("The access token does not identify a user.")
		}

		ctx, cancel := context.WithTimeout(ctx, mcpToolTimeout)
		defer cancel()

		user, err := api.resolveUser(ctx, identity.Email, identity.Name, identity.Picture)
		if errors.Is(err, errReservedAccount) {
			return fail("This account is reserved and cannot be used here.")
		}
		if err != nil {
			log.Printf("mcp: %s: %v", name, err)
			return fail("The user's account could not be loaded. Try again shortly.")
		}

		out, summary, err := run(ctx, user, in)
		var invalid invalidInputError
		var failure *toolError
		switch {
		case errors.As(err, &invalid):
			return fail(invalid.Error())
		case errors.As(err, &failure):
			if failure.cause != nil {
				log.Printf("mcp: %s: %v", name, failure.cause)
			}
			return fail(failure.message)
		case err != nil:
			log.Printf("mcp: %s: %v", name, err)
			return fail("Something went wrong on the server. Try again shortly.")
		}

		text, err := json.Marshal(out)
		if err != nil {
			log.Printf("mcp: %s: encoding the result: %v", name, err)
			return fail("Something went wrong on the server. Try again shortly.")
		}
		return &mcp.CallToolResult{Content: []mcp.Content{
			&mcp.TextContent{Text: summary},
			&mcp.TextContent{Text: string(text)},
		}}, out, nil
	})
}

// schemaOf returns the JSON Schema of T, with property descriptions taken
// from the "jsonschema" struct tags, after describe has adjusted it.
func schemaOf[T any](describe func(properties map[string]*jsonschema.Schema)) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		// The types are fixed at compile time; this cannot depend on input.
		panic(fmt.Sprintf("mcp: schema of %T: %v", *new(T), err))
	}
	arraysNotNull(schema)
	if describe != nil {
		describe(schema.Properties)
	}
	return schema
}

// arraysNotNull turns "array or null", which is how a Go slice is described,
// into "array". The tools never send null for a list, and should not be sent
// one: for the tags of an item, null and "no tags" must not look alike.
func arraysNotNull(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	if len(schema.Types) == 2 && slices.Contains(schema.Types, "null") && slices.Contains(schema.Types, "array") {
		schema.Type, schema.Types = "array", nil
	}
	for _, property := range schema.Properties {
		arraysNotNull(property)
	}
	arraysNotNull(schema.Items)
}

var (
	statusEnum = []any{models.WatchlistStatusPlanned, models.WatchlistStatusWatching, models.WatchlistStatusFinished}
	typeEnum   = []any{models.WatchlistTypeMovie, models.WatchlistTypeShow}
)

// bounded limits an integer property to [min, max] and gives it a default.
func bounded(property *jsonschema.Schema, min, max, def int) {
	property.Minimum, property.Maximum = floatPointer(float64(min)), floatPointer(float64(max))
	property.Default = json.RawMessage(fmt.Sprint(def))
}

func boolPointer(b bool) *bool        { return &b }
func intPointer(n int) *int           { return &n }
func floatPointer(f float64) *float64 { return &f }

// errItemNotFound answers an id that is not on the caller's watchlist,
// whether it exists on someone else's or nowhere: the two must read alike.
var errItemNotFound = invalidInputError("No item with that id is on the user's watchlist.")

// parseItemID reads a watchlist item's id as the tools hand it out.
func parseItemID(id string) (primitive.ObjectID, error) {
	itemID, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return primitive.NilObjectID, invalidInputError("That is not a watchlist item id. Use the id of an item from list_watchlist.")
	}
	return itemID, nil
}

// --- what the tools return ---

// mcpItem is a watchlist item as the tools show it: enough to talk about the
// item and to act on it, without posters, synopses or timestamps.
type mcpItem struct {
	ID          string   `json:"id" jsonschema:"the item's id; pass it to update_watchlist_item or remove_from_watchlist"`
	Title       string   `json:"title"`
	Year        int      `json:"year,omitempty" jsonschema:"release year, when known"`
	Type        string   `json:"type" jsonschema:"movie or show"`
	Status      string   `json:"status" jsonschema:"planned, watching or finished"`
	Tags        []string `json:"tags" jsonschema:"the user's own labels for the item"`
	TMDbID      int      `json:"tmdb_id,omitempty" jsonschema:"the title's id on TMDb; absent for an item that is not linked to TMDb"`
	StreamingOn []string `json:"streaming_on" jsonschema:"the user's own streaming services that have the title now; empty when none of them does or when availability is not known"`
}

func mcpItemOf(row *watchlist.Item) mcpItem {
	item := mcpItemOfStored(row.WatchlistItem)
	if row.Availability != nil {
		for _, provider := range row.Availability.Providers {
			item.StreamingOn = append(item.StreamingOn, provider.Name)
		}
	}
	return item
}

// mcpItemOfStored describes an item whose availability is not at hand.
func mcpItemOfStored(stored *models.WatchlistItem) mcpItem {
	return mcpItem{
		ID:          stored.ID.Hex(),
		Title:       stored.Title,
		Year:        stored.Year,
		Type:        stored.Type,
		Status:      stored.Status,
		Tags:        append([]string{}, stored.Tags...),
		TMDbID:      stored.TMDbID,
		StreamingOn: []string{},
	}
}

// describeItem names an item in a sentence: The Matrix (1999).
func describeItem(item mcpItem) string {
	if item.Year > 0 {
		return fmt.Sprintf("%s (%d)", item.Title, item.Year)
	}
	return item.Title
}

// listed returns the first item of the user's watchlist that match accepts,
// joined with its availability, or nil. It goes through the lister, so the
// answer is the same row list_watchlist would show.
func (s *Server) listed(ctx context.Context, user *models.User, match func(*watchlist.Item) bool) (*watchlist.Item, error) {
	view, err := s.lister.List(ctx, user, watchlist.Options{})
	if err != nil {
		return nil, err
	}
	for i := range view.Items {
		if match(&view.Items[i]) {
			return &view.Items[i], nil
		}
	}
	return nil, nil
}

// itemAsListed describes a stored item the way list_watchlist would, with
// its availability when the list can be read.
func (s *Server) itemAsListed(ctx context.Context, user *models.User, stored *models.WatchlistItem) mcpItem {
	row, err := s.listed(ctx, user, func(it *watchlist.Item) bool { return it.ID == stored.ID })
	if err != nil || row == nil {
		// The write succeeded; the answer just goes without availability.
		return mcpItemOfStored(stored)
	}
	return mcpItemOf(row)
}

// --- list_watchlist ---

type listWatchlistInput struct {
	Status        string `json:"status,omitempty" jsonschema:"only items with this status: planned (not started), watching or finished"`
	Type          string `json:"type,omitempty" jsonschema:"only movies or only shows"`
	Query         string `json:"query,omitempty" jsonschema:"only items whose title contains this text, ignoring case"`
	StreamingOnly bool   `json:"streaming_only,omitempty" jsonschema:"when true, only items streaming right now on at least one of the user's own services"`
	Limit         int    `json:"limit,omitempty" jsonschema:"how many items to return"`
	Offset        int    `json:"offset,omitempty" jsonschema:"how many matching items to skip; use next_offset from the previous page"`
}

type listWatchlistOutput struct {
	Items      []mcpItem `json:"items"`
	Total      int       `json:"total" jsonschema:"how many items match the filters, across all pages"`
	Offset     int       `json:"offset"`
	NextOffset int       `json:"next_offset,omitempty" jsonschema:"the offset of the next page; absent on the last page"`
	Region     string    `json:"region" jsonschema:"the country streaming availability is reported for"`
}

func (s *Server) mcpListWatchlist(ctx context.Context, user *models.User, in listWatchlistInput) (listWatchlistOutput, string, error) {
	var out listWatchlistOutput
	if !validWatchlistStatus(in.Status) {
		return out, "", invalidInputError("status must be planned, watching or finished.")
	}
	if !validWatchlistType(in.Type) {
		return out, "", invalidInputError("type must be movie or show.")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = mcpListDefaultLimit
	}
	opts := watchlist.Options{
		Status: in.Status,
		Type:   in.Type,
		Search: in.Query,
		Limit:  min(limit, mcpListMaxLimit),
		Offset: max(in.Offset, 0),
	}
	if in.StreamingOnly {
		opts.Available = boolPointer(true)
	}

	view, err := s.lister.List(ctx, user, opts)
	if err != nil {
		return out, "", err
	}
	out = listWatchlistOutput{
		Items:  make([]mcpItem, 0, len(view.Items)),
		Total:  view.Total,
		Offset: opts.Offset,
		Region: view.Region,
	}
	for i := range view.Items {
		out.Items = append(out.Items, mcpItemOf(&view.Items[i]))
	}
	summary := fmt.Sprintf("%d of %d matching watchlist items.", len(out.Items), out.Total)
	if next := opts.Offset + len(out.Items); next < out.Total {
		out.NextOffset = next
		summary += fmt.Sprintf(" More from offset %d.", next)
	}
	return out, summary, nil
}

// --- whats_streaming ---

type whatsStreamingInput struct {
	Type string `json:"type,omitempty" jsonschema:"only movies or only shows"`
}

type whatsStreamingOutput struct {
	Services     []streamingService `json:"services" jsonschema:"the user's services that have at least one watchlist title, by name"`
	Streaming    int                `json:"streaming" jsonschema:"how many watchlist titles are on at least one of the user's services"`
	NotStreaming int                `json:"not_streaming" jsonschema:"how many are on none of them"`
	YourServices []string           `json:"your_services" jsonschema:"the streaming services the user has; when empty, the user has chosen none in the app and nothing can match"`
	Region       string             `json:"region" jsonschema:"the country streaming availability is reported for"`
}

type streamingService struct {
	Service string           `json:"service" jsonschema:"the service's name"`
	Titles  []streamingTitle `json:"titles"`
}

type streamingTitle struct {
	ID     string `json:"id" jsonschema:"the watchlist item's id"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
	Type   string `json:"type" jsonschema:"movie or show"`
	Status string `json:"status" jsonschema:"planned, watching or finished"`
}

func (s *Server) mcpWhatsStreaming(ctx context.Context, user *models.User, in whatsStreamingInput) (whatsStreamingOutput, string, error) {
	var out whatsStreamingOutput
	if !validWatchlistType(in.Type) {
		return out, "", invalidInputError("type must be movie or show.")
	}
	view, err := s.lister.List(ctx, user, watchlist.Options{Type: in.Type, Available: boolPointer(true), Sort: "title"})
	if err != nil {
		return out, "", err
	}

	out = whatsStreamingOutput{
		Services:  []streamingService{},
		Streaming: view.Total,
		// The availability counts describe the list before the availability
		// filter, so this is the other half of the same items.
		NotStreaming: view.Facets.Unavailable,
		YourServices: []string{},
		Region:       view.Region,
	}
	for code := range user.ActiveServiceCodes() {
		out.YourServices = append(out.YourServices, models.ServiceName(code))
	}
	sort.Strings(out.YourServices)

	var counts []string
	for _, group := range watchlist.GroupByService(view.Items) {
		service := streamingService{Service: group.Name, Titles: make([]streamingTitle, 0, len(group.Items))}
		for _, item := range group.Items {
			service.Titles = append(service.Titles, streamingTitle{
				ID: item.ID.Hex(), Title: item.Title, Year: item.Year, Type: item.Type, Status: item.Status,
			})
		}
		out.Services = append(out.Services, service)
		counts = append(counts, fmt.Sprintf("%s: %d", group.Name, len(group.Items)))
	}

	switch {
	case len(out.YourServices) == 0:
		return out, "The user has not chosen any streaming services in Watchlist Notify, so nothing can be matched. They are chosen in the app's settings.", nil
	case out.Streaming == 0:
		return out, fmt.Sprintf("Nothing on the watchlist is streaming on the user's services right now (%s).", strings.Join(out.YourServices, ", ")), nil
	}
	return out, fmt.Sprintf("%d watchlist titles are streaming on the user's services (%s); %d are not.",
		out.Streaming, strings.Join(counts, ", "), out.NotStreaming), nil
}

// --- search_titles ---

type searchTitlesInput struct {
	Query string `json:"query" jsonschema:"the name of the movie or show, or part of it"`
	Type  string `json:"type,omitempty" jsonschema:"only movies or only shows; both when left out"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many results to return"`
}

type searchTitlesOutput struct {
	Results []searchResult `json:"results" jsonschema:"best matches first"`
}

type searchResult struct {
	TMDbID   int    `json:"tmdb_id" jsonschema:"the title's id on TMDb; pass it to add_to_watchlist together with type"`
	Type     string `json:"type" jsonschema:"movie or show"`
	Title    string `json:"title"`
	Year     int    `json:"year,omitempty" jsonschema:"release year, when known"`
	Overview string `json:"overview,omitempty" jsonschema:"the start of the synopsis, to tell apart titles with the same name"`
}

func (s *Server) mcpSearchTitles(ctx context.Context, _ *models.User, in searchTitlesInput) (searchTitlesOutput, string, error) {
	out := searchTitlesOutput{Results: []searchResult{}}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return out, "", invalidInputError("query is required.")
	}
	if utf8.RuneCountInString(query) > mcpMaxQueryLen {
		return out, "", invalidInputError("query is too long.")
	}
	// TMDb searches movies and TV separately, under its own names for them.
	var mediaTypes []string
	switch in.Type {
	case "":
		mediaTypes = []string{models.MediaTypeMovie, models.MediaTypeTV}
	case models.WatchlistTypeMovie, models.WatchlistTypeShow:
		mediaType, _ := models.TMDbMediaType(in.Type)
		mediaTypes = []string{mediaType}
	default:
		return out, "", invalidInputError("type must be movie or show.")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = mcpSearchDefaultLimit
	}
	limit = min(limit, mcpSearchMaxLimit)

	// The same keys GET /api/search uses with its defaults, so the two share
	// cache entries.
	pages := make([]*cache.SearchValue, len(mediaTypes))
	errs := make([]error, len(mediaTypes))
	var wg sync.WaitGroup
	for i, mediaType := range mediaTypes {
		wg.Go(func() {
			pages[i], errs[i] = s.searchTitles(ctx, cache.SearchKey{Query: query, Page: 1, Region: searchRegion(""), Type: mediaType})
		})
	}
	wg.Wait()
	// Half an answer would read as "there is no such show".
	if i := slices.IndexFunc(errs, func(err error) bool { return err != nil }); i >= 0 {
		err := errs[i]
		switch {
		case errors.Is(err, errSearchUnavailable):
			return out, "", &toolError{message: "Title search is not available on this server."}
		case errors.Is(err, errSearchTimedOut):
			return out, "", &toolError{message: "The title search timed out. Try again.", cause: err}
		default:
			return out, "", &toolError{message: "The title search failed. Try again shortly.", cause: err}
		}
	}

	// Movies and shows each come ranked; take them alternately so that the
	// best of both are at the top.
	for rank := 0; len(out.Results) < limit; rank++ {
		more := false
		for _, page := range pages {
			if rank >= len(page.Results) || len(out.Results) >= limit {
				continue
			}
			more = true
			found := page.Results[rank]
			itemType := models.WatchlistTypeMovie
			if found.Type == models.MediaTypeTV {
				itemType = models.WatchlistTypeShow
			}
			out.Results = append(out.Results, searchResult{
				TMDbID: found.TMDBID, Type: itemType, Title: found.Title, Year: found.Year,
				Overview: oneLine(found.Overview, mcpOverviewLen),
			})
		}
		if !more {
			break
		}
	}
	if len(out.Results) == 0 {
		return out, fmt.Sprintf("No titles found for %q.", query), nil
	}
	return out, fmt.Sprintf("%d titles found for %q.", len(out.Results), query), nil
}

// oneLine collapses text onto one line of at most limit characters.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

// --- add_to_watchlist ---

type addToWatchlistInput struct {
	TMDbID int    `json:"tmdb_id" jsonschema:"the title's id on TMDb, from search_titles"`
	Type   string `json:"type" jsonschema:"movie or show, from the same search result; ids are only unique within a type"`
}

type addToWatchlistOutput struct {
	Item               mcpItem `json:"item"`
	AlreadyOnWatchlist bool    `json:"already_on_watchlist" jsonschema:"true when the title was on the watchlist before this call, which then changed nothing"`
}

func (s *Server) mcpAddToWatchlist(ctx context.Context, user *models.User, in addToWatchlistInput) (addToWatchlistOutput, string, error) {
	var out addToWatchlistOutput
	if in.Type != models.WatchlistTypeMovie && in.Type != models.WatchlistTypeShow {
		return out, "", invalidInputError("type must be movie or show.")
	}
	if in.TMDbID <= 0 {
		return out, "", invalidInputError("tmdb_id must be a positive number. Use search_titles to find it.")
	}
	sameTitle := func(it *watchlist.Item) bool { return it.TMDbID == in.TMDbID && it.Type == in.Type }
	already := func(row *watchlist.Item) (addToWatchlistOutput, string, error) {
		item := mcpItemOf(row)
		return addToWatchlistOutput{Item: item, AlreadyOnWatchlist: true},
			fmt.Sprintf("%s is already on the watchlist (%s). Nothing was changed.", describeItem(item), item.Status), nil
	}

	existing, err := s.listed(ctx, user, sameTitle)
	if err != nil {
		return out, "", err
	}
	if existing != nil {
		return already(existing)
	}

	// The caller gave an id, not a name: the name and year come from TMDb.
	meta, err := s.titleMetadata(ctx, user, in.Type, in.TMDbID)
	if err != nil {
		return out, "", err
	}
	stored, err := s.addWatchlistItem(ctx, user, newWatchlistItem{
		Title: meta.Title, Type: in.Type, Year: meta.Year, TMDbID: &in.TMDbID,
	})
	if errors.Is(err, database.ErrDuplicateWatchlistItem) {
		// Another request added it between the check and the insert. The
		// unique index kept the list right; report it as the no-op it is.
		existing, err = s.listed(ctx, user, sameTitle)
		if err == nil && existing != nil {
			return already(existing)
		}
		return out, "", &toolError{message: "The title is already on the watchlist.", cause: err}
	}
	if err != nil {
		return out, "", err
	}

	out.Item = s.itemAsListed(ctx, user, stored)
	summary := fmt.Sprintf("Added %s to the watchlist.", describeItem(out.Item))
	if len(out.Item.StreamingOn) > 0 {
		summary += fmt.Sprintf(" It is streaming on %s.", strings.Join(out.Item.StreamingOn, ", "))
	}
	return out, summary, nil
}

// titleMetadata returns what TMDb knows about a title, to name a new
// watchlist item with. A title the API already stores is not fetched again.
// Otherwise it is fetched within the upstream lookup budget and stored with
// its availability, which is what adding the item would do next anyway.
func (s *Server) titleMetadata(ctx context.Context, user *models.User, itemType string, tmdbID int) (models.TitleMetadata, error) {
	mediaType, _ := models.TMDbMediaType(itemType)
	ref := models.TitleRef{MediaType: mediaType, TMDbID: tmdbID}
	region := user.RegionOr(s.defaultRegion)

	titles, err := s.db.GetTitles(ctx, []string{ref.Key()}, []string{region})
	if err != nil {
		return models.TitleMetadata{}, err
	}
	stored := titles[ref.Key()]
	if stored != nil && stored.NotFoundAt == nil && stored.Title != "" {
		return stored.TitleMetadata, nil
	}
	if !s.refresher.CanFetch() {
		return models.TitleMetadata{}, &toolError{message: "Titles cannot be looked up on this server, so nothing was added."}
	}

	lookupCtx, cancel := s.lookupContext(ctx)
	defer cancel()
	refreshed, err := s.refresher.Refresh(lookupCtx, ref, stored, []string{region})
	switch {
	case errors.Is(err, availability.ErrTitleNotFound):
		return models.TitleMetadata{}, invalidInputError(fmt.Sprintf(
			"TMDb has no %s with id %d. Use search_titles to find the right tmdb_id and type.", itemType, tmdbID))
	case err != nil && errors.Is(lookupCtx.Err(), context.DeadlineExceeded):
		return models.TitleMetadata{}, &toolError{message: "Looking the title up timed out, so nothing was added. Try again.", cause: err}
	case err != nil:
		return models.TitleMetadata{}, &toolError{message: "The title could not be looked up, so nothing was added. Try again shortly.", cause: err}
	case refreshed.Title == nil || refreshed.Title.Title == "":
		return models.TitleMetadata{}, &toolError{message: "TMDb has no name for that title, so it was not added."}
	}
	return refreshed.Title.TitleMetadata, nil
}

// --- update_watchlist_item ---

type updateWatchlistItemInput struct {
	ID     string   `json:"id" jsonschema:"the id of the item, from list_watchlist"`
	Status string   `json:"status,omitempty" jsonschema:"the new status: planned (not started), watching or finished"`
	Tags   []string `json:"tags,omitempty" jsonschema:"the new tags, replacing all current ones; an empty list removes every tag"`
}

type updateWatchlistItemOutput struct {
	Item mcpItem `json:"item" jsonschema:"the item after the change"`
}

func (s *Server) mcpUpdateWatchlistItem(ctx context.Context, user *models.User, in updateWatchlistItemInput) (updateWatchlistItemOutput, string, error) {
	var out updateWatchlistItemOutput
	itemID, err := parseItemID(in.ID)
	if err != nil {
		return out, "", err
	}
	var patch watchlistItemPatch
	if in.Status != "" {
		patch.Status = &in.Status
	}
	if in.Tags != nil {
		if patch.Tags, err = cleanTags(in.Tags); err != nil {
			return out, "", err
		}
	}
	if patch.Status == nil && patch.Tags == nil {
		return out, "", invalidInputError("Nothing to change: give a status, tags, or both.")
	}

	// The update's filter is the item's id together with the user's id: an
	// id from someone else's watchlist matches nothing.
	stored, err := s.updateWatchlistItem(ctx, user.ID, itemID, patch)
	switch {
	case errors.Is(err, database.ErrWatchlistItemNotFound):
		return out, "", errItemNotFound
	case err != nil:
		// An invalidInputError passes through with its own message.
		return out, "", err
	}

	out.Item = s.itemAsListed(ctx, user, stored)
	var changes []string
	if patch.Status != nil {
		changes = append(changes, "status "+out.Item.Status)
	}
	if patch.Tags != nil {
		if len(out.Item.Tags) == 0 {
			changes = append(changes, "no tags")
		} else {
			changes = append(changes, "tags "+strings.Join(out.Item.Tags, ", "))
		}
	}
	return out, fmt.Sprintf("Updated %s: %s.", describeItem(out.Item), strings.Join(changes, "; ")), nil
}

// cleanTags trims tags and drops empty and repeated ones, keeping the order.
// The result is never nil: no tags left means "remove them all".
func cleanTags(tags []string) ([]string, error) {
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || slices.Contains(cleaned, tag) {
			continue
		}
		if utf8.RuneCountInString(tag) > mcpMaxTagLen {
			return nil, invalidInputError(fmt.Sprintf("A tag can be at most %d characters long.", mcpMaxTagLen))
		}
		cleaned = append(cleaned, tag)
	}
	if len(cleaned) > mcpMaxTags {
		return nil, invalidInputError(fmt.Sprintf("An item can have at most %d tags.", mcpMaxTags))
	}
	return cleaned, nil
}

// --- remove_from_watchlist ---

type removeFromWatchlistInput struct {
	ID string `json:"id" jsonschema:"the id of the item to remove, from list_watchlist"`
}

type removeFromWatchlistOutput struct {
	Removed bool   `json:"removed"`
	ID      string `json:"id" jsonschema:"the id of the item that was removed"`
}

func (s *Server) mcpRemoveFromWatchlist(ctx context.Context, user *models.User, in removeFromWatchlistInput) (removeFromWatchlistOutput, string, error) {
	var out removeFromWatchlistOutput
	itemID, err := parseItemID(in.ID)
	if err != nil {
		return out, "", err
	}
	// The delete's filter is the item's id together with the user's id. An
	// id from someone else's watchlist deletes nothing and is answered like
	// an id that does not exist, so the answer does not reveal which it was.
	err = s.db.DeleteWatchlistItem(ctx, user.ID, itemID)
	switch {
	case errors.Is(err, database.ErrWatchlistItemNotFound):
		return out, "", errItemNotFound
	case err != nil:
		return out, "", err
	}
	return removeFromWatchlistOutput{Removed: true, ID: itemID.Hex()}, "Removed the item from the watchlist.", nil
}
