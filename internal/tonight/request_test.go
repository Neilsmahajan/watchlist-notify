package tonight

import (
	"errors"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func intPointer(n int) *int { return &n }

func ids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = primitive.NewObjectID().Hex()
	}
	return out
}

func TestNewRequest(t *testing.T) {
	id := primitive.NewObjectID()
	req, err := NewRequest("  Something\n light\t", intPointer(120), "movie", []string{id.Hex(), id.Hex()})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if req.Mood != "Something light" || req.MaxMinutes != 120 || req.Type != "movie" {
		t.Errorf("request = %+v", req)
	}
	if len(req.Exclude) != 1 || !req.Exclude[id] {
		t.Errorf("exclude = %v, want the one ID", req.Exclude)
	}

	// Everything is optional.
	req, err = NewRequest("", nil, "", nil)
	if err != nil || req.Mood != "" || req.MaxMinutes != 0 || req.Type != "" || req.Exclude != nil {
		t.Errorf("empty request = %+v, %v", req, err)
	}
}

func TestNewRequestBounds(t *testing.T) {
	cases := []struct {
		name       string
		mood       string
		maxMinutes *int
		mediaType  string
		exclude    []string
		wantErr    string // empty: accepted
	}{
		{name: "mood at the limit", mood: strings.Repeat("é", MaxMoodLength)},
		{name: "mood over the limit", mood: strings.Repeat("é", MaxMoodLength+1), wantErr: "mood is too long"},
		// Padding does not count: the limit is on what is sent.
		{name: "padded mood", mood: strings.Repeat(" ", 300) + "light"},
		{name: "show", mediaType: "show"},
		{name: "unknown type", mediaType: "tv", wantErr: "type must be"},
		{name: "one minute", maxMinutes: intPointer(1)},
		{name: "longest", maxMinutes: intPointer(MaxMinutes)},
		{name: "zero minutes", maxMinutes: intPointer(0), wantErr: "max_minutes"},
		{name: "negative minutes", maxMinutes: intPointer(-90), wantErr: "max_minutes"},
		{name: "too many minutes", maxMinutes: intPointer(MaxMinutes + 1), wantErr: "max_minutes"},
		{name: "exclude at the limit", exclude: ids(MaxExclude)},
		{name: "exclude over the limit", exclude: ids(MaxExclude + 1), wantErr: "exclude holds at most"},
		{name: "exclude that is not an id", exclude: []string{"The Matrix"}, wantErr: "exclude must hold"},
		{name: "empty id in exclude", exclude: []string{""}, wantErr: "exclude must hold"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRequest(tc.mood, tc.maxMinutes, tc.mediaType, tc.exclude)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("rejected: %v", err)
				}
				return
			}
			var invalid *InvalidRequestError
			if !errors.As(err, &invalid) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want an *InvalidRequestError about %q", err, tc.wantErr)
			}
			// The text is shown to the caller and may be logged: it describes
			// the rule, never the value.
			if tc.mood != "" && strings.Contains(err.Error(), tc.mood[:4]) {
				t.Errorf("the error repeats the mood: %q", err.Error())
			}
		})
	}
}

func TestCleanTextAndClip(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"  two   spaces\n\nand\tlines  ", "two spaces and lines"},
		{"null\x00byte and bell\x07", "null byte and bell"},
		{"zero​width and ‮override", "zero width and override"},
		{"émoji 🎬 stays", "émoji 🎬 stays"},
		{"\n\t ", ""},
	}
	for _, tc := range cases {
		if got := cleanText(tc.in); got != tc.want {
			t.Errorf("cleanText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if got := clip("short", 10); got != "short" {
		t.Errorf("clip left a short text as %q", got)
	}
	if got := clip("exactly ten", 11); got != "exactly ten" {
		t.Errorf("clip cut a text at the limit: %q", got)
	}
	if got := clip("one two three", 8); got != "one two…" || len([]rune(got)) != 8 {
		t.Errorf("clip = %q, want 8 characters ending in an ellipsis", got)
	}
	if got := clip("ééééé", 3); got != "éé…" {
		t.Errorf("clip counts bytes, not characters: %q", got)
	}
}
