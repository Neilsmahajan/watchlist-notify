package main

import (
	"testing"
	"time"
)

func TestRefreshMinAge(t *testing.T) {
	tests := []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{"", defaultRefreshMinAge, false},
		{"  ", defaultRefreshMinAge, false},
		{"20h", 20 * time.Hour, false},
		{" 90m ", 90 * time.Minute, false},
		{"0", 0, false},
		{"-1h", 0, true},
		{"daily", 0, true},
		{"20", 0, true},
	}
	for _, tt := range tests {
		got, err := refreshMinAge(tt.value)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("refreshMinAge(%q) = %v, %v; want %v, error: %v", tt.value, got, err, tt.want, tt.wantErr)
		}
	}
}
