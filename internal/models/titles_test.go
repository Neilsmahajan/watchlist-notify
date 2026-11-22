package models

import (
	"reflect"
	"testing"
	"time"
)

func TestTitleRefKey(t *testing.T) {
	tests := []struct {
		itemType string
		tmdbID   int
		wantKey  string
		wantOK   bool
	}{
		{WatchlistTypeMovie, 603, "movie:603", true},
		{WatchlistTypeShow, 1399, "tv:1399", true},
		{WatchlistTypeMovie, 0, "", false},
		{"podcast", 5, "", false},
	}
	for _, tt := range tests {
		item := &WatchlistItem{Type: tt.itemType, TMDbID: tt.tmdbID}
		ref, ok := item.TitleRef()
		if ok != tt.wantOK {
			t.Errorf("TitleRef(%s, %d) ok = %v, want %v", tt.itemType, tt.tmdbID, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if ref.Key() != tt.wantKey {
			t.Errorf("TitleRef(%s, %d).Key() = %q, want %q", tt.itemType, tt.tmdbID, ref.Key(), tt.wantKey)
		}
		parsed, err := ParseTitleKey(ref.Key())
		if err != nil || parsed != ref {
			t.Errorf("ParseTitleKey(%q) = %+v, %v; want %+v", ref.Key(), parsed, err, ref)
		}
	}
}

func TestParseTitleKeyRejectsMalformed(t *testing.T) {
	for _, key := range []string{"", "movie", "movie:", "movie:abc", "movie:-1", "book:1", "tv:0"} {
		if _, err := ParseTitleKey(key); err == nil {
			t.Errorf("ParseTitleKey(%q) succeeded, want an error", key)
		}
	}
}

func TestNormalizeRegion(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"US", "US", true},
		{" us ", "US", true},
		{"gb", "GB", true},
		{"", "", false},
		{"USA", "", false},
		{"U", "", false},
		{"U$", "", false},
		{"1A", "", false},
		{"..", "", false},
	}
	for _, tt := range tests {
		got, ok := NormalizeRegion(tt.in)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("NormalizeRegion(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestSnapshotServices(t *testing.T) {
	snap := RegionSnapshot{
		FetchedAt: time.Now(),
		Offers: []ProviderOffer{
			{ProviderID: 2303, ProviderName: "Paramount Plus Premium", Access: AccessSubscription},
			{ProviderID: 2616, ProviderName: "Paramount Plus Essential", Access: AccessSubscription},
			{ProviderID: 999010, ProviderName: "Paramount+ Amazon Channel", Access: AccessSubscription},
			{ProviderID: 73, ProviderName: "Tubi TV", Access: AccessFree},
			{ProviderID: 73, ProviderName: "Tubi TV", Access: AccessAds},
			{ProviderID: 8, ProviderName: "Netflix", Access: AccessRent},
			{ProviderID: 10, ProviderName: "Amazon Video", Access: AccessBuy},
		},
	}

	got := make(map[string][]string)
	for _, svc := range snap.Services() {
		got[svc.Code] = svc.Access
	}
	want := map[string][]string{
		ServiceParamountPlus: {AccessSubscription},
		ServiceTubi:          {AccessFree, AccessAds},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Services() = %v, want %v", got, want)
	}
	if !snap.HasService(ServiceTubi) {
		t.Error("HasService(tubi) = false, want true")
	}
	if snap.HasService(ServiceNetflix) {
		t.Error("HasService(netflix) = true for a rent-only offer, want false")
	}
}

func TestTitleSnapshotIsNilSafe(t *testing.T) {
	var title *Title
	if _, ok := title.Snapshot("US"); ok {
		t.Error("nil title reported a snapshot")
	}
}

func TestUserHelpers(t *testing.T) {
	u := &User{
		Email:  "account@example.com",
		Region: "gb",
		Services: []ServiceSubscription{
			{Code: "netflix", Active: true},
			{Code: "hulu", Active: false},
			{Code: "MAX", Active: true},
		},
	}
	if got, want := u.ActiveServiceCodes(), map[string]bool{"netflix": true, "max": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("ActiveServiceCodes() = %v, want %v", got, want)
	}
	if got := u.RegionOr("US"); got != "GB" {
		t.Errorf("RegionOr = %q, want GB", got)
	}
	u.Region = "nowhere"
	if got := u.RegionOr("US"); got != "US" {
		t.Errorf("RegionOr with an invalid region = %q, want US", got)
	}

	if got := u.DigestRecipient(); got != "account@example.com" {
		t.Errorf("DigestRecipient = %q, want the account email", got)
	}
	u.Preferences.NotifyEmail = "other@example.com"
	u.Preferences.UseAccountEmail = true
	if got := u.DigestRecipient(); got != "account@example.com" {
		t.Errorf("DigestRecipient with use_account_email = %q, want the account email", got)
	}
	u.Preferences.UseAccountEmail = false
	if got := u.DigestRecipient(); got != "other@example.com" {
		t.Errorf("DigestRecipient = %q, want the notification email", got)
	}
}
