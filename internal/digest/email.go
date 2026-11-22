package digest

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
)

const (
	// MaxEmailEntries caps how many titles one email lists; the rest are
	// summarised as a count with a link to the watchlist.
	MaxEmailEntries = 20

	posterBaseURL = "https://image.tmdb.org/t/p/w154"
)

// Email is a rendered digest.
type Email struct {
	Subject string
	HTML    string
}

// emailEntry is one title as the template sees it.
type emailEntry struct {
	Title     string
	Year      int
	PosterURL string
	Services  string
}

type emailData struct {
	Subject        string
	Heading        string
	Lead           string
	Entries        []emailEntry
	More           int
	WatchlistURL   string
	PreferencesURL string
}

// Subject returns the subject line for a digest of n titles.
func Subject(n int) string {
	if n == 1 {
		return "1 title on your watchlist is now streaming"
	}
	return fmt.Sprintf("%d titles on your watchlist are now streaming", n)
}

// Render builds the digest email for entries. siteURL is the public address
// of the web app, used for the watchlist and preferences links.
//
// Everything user- or TMDb-supplied goes through html/template, so a title
// such as `<script>` arrives in the inbox as text.
func Render(siteURL string, entries []Entry) (Email, error) {
	if len(entries) == 0 {
		return Email{}, fmt.Errorf("digest: nothing to render")
	}
	siteURL = strings.TrimRight(strings.TrimSpace(siteURL), "/")

	data := emailData{
		Subject:        Subject(len(entries)),
		WatchlistURL:   siteURL + "/watchlist",
		PreferencesURL: siteURL + "/settings",
	}
	data.Heading = "Now streaming on your services"
	data.Lead = "These titles on your watchlist became available since your last digest."
	if len(entries) == 1 {
		data.Lead = "A title on your watchlist became available since your last digest."
	}

	shown := entries
	if len(shown) > MaxEmailEntries {
		shown = shown[:MaxEmailEntries]
		data.More = len(entries) - MaxEmailEntries
	}
	for _, entry := range shown {
		data.Entries = append(data.Entries, emailEntry{
			Title:     entry.Title,
			Year:      entry.Year,
			PosterURL: posterURL(entry.PosterPath),
			Services:  joinNames(entry.ServiceNames()),
		})
	}

	var buf bytes.Buffer
	if err := emailTemplate.Execute(&buf, data); err != nil {
		return Email{}, fmt.Errorf("digest: render email: %w", err)
	}
	return Email{Subject: data.Subject, HTML: buf.String()}, nil
}

// posterURL turns a TMDb poster path ("/abc.jpg") into an image URL, or ""
// when the path is missing or not shaped like one.
func posterURL(path string) string {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " \t\r\n?#\"'<>\\") || strings.Contains(path, "..") {
		return ""
	}
	return posterBaseURL + path
}

// joinNames renders "A", "A and B" or "A, B and C".
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// The email is deliberately plain: one table, inline styles for clients that
// drop <style>, and a small media query for the ones that honour dark mode.
var emailTemplate = template.Must(template.New("digest").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="supported-color-schemes" content="light dark">
<title>{{.Subject}}</title>
<style>
@media (prefers-color-scheme: dark) {
  .wn-body { background-color: #111418 !important; color: #e6e8eb !important; }
  .wn-text { color: #e6e8eb !important; }
  .wn-muted { color: #9aa3ad !important; }
  .wn-rule { border-color: #2a3038 !important; }
  .wn-link { color: #8ab4ff !important; }
  .wn-poster { background-color: #1c2128 !important; }
}
</style>
</head>
<body class="wn-body" style="margin:0;padding:0;background-color:#ffffff;color:#1c2024;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
<tr><td align="center" style="padding:24px 16px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:560px;">
<tr><td style="padding-bottom:16px;">
<h1 class="wn-text" style="margin:0;font-size:20px;line-height:28px;font-weight:600;color:#1c2024;">{{.Heading}}</h1>
<p class="wn-muted" style="margin:4px 0 0;font-size:14px;line-height:20px;color:#5f6b7a;">{{.Lead}}</p>
</td></tr>
{{range .Entries}}<tr><td class="wn-rule" style="padding:12px 0;border-top:1px solid #e3e6ea;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>
<td width="62" valign="top" style="padding-right:14px;">{{if .PosterURL}}<img class="wn-poster" src="{{.PosterURL}}" width="62" height="93" alt="" style="display:block;border:0;border-radius:4px;background-color:#eef0f3;">{{else}}<div class="wn-poster" style="width:62px;height:93px;border-radius:4px;background-color:#eef0f3;"></div>{{end}}</td>
<td valign="middle">
<p class="wn-text" style="margin:0;font-size:16px;line-height:22px;font-weight:600;color:#1c2024;">{{.Title}}{{if .Year}} <span class="wn-muted" style="font-weight:400;color:#5f6b7a;">({{.Year}})</span>{{end}}</p>
<p class="wn-muted" style="margin:4px 0 0;font-size:14px;line-height:20px;color:#5f6b7a;">Now on {{.Services}}</p>
</td>
</tr></table>
</td></tr>
{{end}}{{if .More}}<tr><td class="wn-rule" style="padding:12px 0;border-top:1px solid #e3e6ea;">
<p class="wn-muted" style="margin:0;font-size:14px;line-height:20px;color:#5f6b7a;">And {{.More}} more on your watchlist.</p>
</td></tr>
{{end}}<tr><td class="wn-rule" style="padding:16px 0;border-top:1px solid #e3e6ea;">
<a class="wn-link" href="{{.WatchlistURL}}" style="font-size:15px;line-height:22px;font-weight:600;color:#1a56db;text-decoration:underline;">Open your watchlist</a>
</td></tr>
<tr><td style="padding-top:8px;">
<p class="wn-muted" style="margin:0;font-size:12px;line-height:18px;color:#5f6b7a;">You get this email because digests are turned on for your Watchlist Notify account. <a class="wn-link" href="{{.PreferencesURL}}" style="color:#1a56db;text-decoration:underline;">Manage email preferences</a>.</p>
<p class="wn-muted" style="margin:6px 0 0;font-size:12px;line-height:18px;color:#5f6b7a;">Streaming availability data from JustWatch, via TMDB.</p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))
