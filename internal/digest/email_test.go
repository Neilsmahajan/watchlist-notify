package digest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

func TestRender(t *testing.T) {
	email, err := Render("https://watchlistnotify.com/", []Entry{
		{Title: "The Matrix", Year: 1999, PosterPath: "/matrix.jpg", ServiceCodes: []string{models.ServiceMax, models.ServiceNetflix}},
		{Title: "Untitled Project", ServiceCodes: []string{models.ServiceAppleTVPlus}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if email.Subject != "2 titles on your watchlist are now streaming" {
		t.Errorf("Subject = %q", email.Subject)
	}
	for _, want := range []string{
		"The Matrix",
		"(1999)",
		"Now on HBO Max and Netflix",
		"Now on Apple TV",
		`src="https://image.tmdb.org/t/p/w154/matrix.jpg"`,
		`href="https://watchlistnotify.com/watchlist"`,
		`href="https://watchlistnotify.com/settings"`,
		"Manage email preferences",
		"prefers-color-scheme: dark",
	} {
		if !strings.Contains(email.HTML, want) {
			t.Errorf("email HTML does not contain %q", want)
		}
	}
	if strings.Contains(email.HTML, "(0)") {
		t.Error("a missing year was rendered as (0)")
	}
	if got := strings.Count(email.HTML, "<img "); got != 1 {
		t.Errorf("email has %d images, want 1 (the entry without a poster gets a placeholder)", got)
	}
}

func TestRenderEscapesTitles(t *testing.T) {
	email, err := Render("https://watchlistnotify.com", []Entry{{
		Title:        `<script>alert("x")</script> & Friends`,
		PosterPath:   `/x.jpg" onerror="alert(1)`,
		ServiceCodes: []string{models.ServiceNetflix},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(email.HTML, "<script>") || strings.Contains(email.HTML, "onerror") {
		t.Errorf("unescaped input reached the email:\n%s", email.HTML)
	}
	if !strings.Contains(email.HTML, "&lt;script&gt;") || !strings.Contains(email.HTML, "&amp; Friends") {
		t.Error("the title was not rendered as escaped text")
	}
	if email.Subject != "1 title on your watchlist is now streaming" {
		t.Errorf("Subject = %q", email.Subject)
	}
}

func TestRenderCapsLongDigests(t *testing.T) {
	var entries []Entry
	for i := 0; i < MaxEmailEntries+5; i++ {
		entries = append(entries, Entry{Title: fmt.Sprintf("Title %02d", i), ServiceCodes: []string{models.ServiceNetflix}})
	}
	email, err := Render("https://watchlistnotify.com", entries)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(email.HTML, "Now on Netflix"); got != MaxEmailEntries {
		t.Errorf("email lists %d titles, want %d", got, MaxEmailEntries)
	}
	if !strings.Contains(email.HTML, "And 5 more on your watchlist.") {
		t.Error("email does not mention the titles it left out")
	}
	if email.Subject != "25 titles on your watchlist are now streaming" {
		t.Errorf("Subject = %q, want it to count every title", email.Subject)
	}
}

func TestRenderNothing(t *testing.T) {
	if _, err := Render("https://watchlistnotify.com", nil); err == nil {
		t.Error("Render with no entries succeeded")
	}
}

func TestPosterURL(t *testing.T) {
	tests := map[string]string{
		"/abc123.jpg":       "https://image.tmdb.org/t/p/w154/abc123.jpg",
		"":                  "",
		"abc.jpg":           "",
		"/a b.jpg":          "",
		"/../secret":        "",
		"/x.jpg?size=huge":  "",
		"//evil.test/x.jpg": "https://image.tmdb.org/t/p/w154//evil.test/x.jpg", // still on TMDb's host
	}
	for in, want := range tests {
		if got := posterURL(in); got != want {
			t.Errorf("posterURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJoinNames(t *testing.T) {
	tests := []struct {
		names []string
		want  string
	}{
		{nil, ""},
		{[]string{"Netflix"}, "Netflix"},
		{[]string{"Hulu", "Netflix"}, "Hulu and Netflix"},
		{[]string{"HBO Max", "Hulu", "Netflix"}, "HBO Max, Hulu and Netflix"},
	}
	for _, tt := range tests {
		if got := joinNames(tt.names); got != tt.want {
			t.Errorf("joinNames(%v) = %q, want %q", tt.names, got, tt.want)
		}
	}
}
