package models

import "testing"

func TestMapProvider(t *testing.T) {
	tests := []struct {
		name     string
		id       int
		provider string
		wantCode string
		wantOK   bool
	}{
		// IDs decide, whatever the provider is called this year.
		{"netflix by id", 8, "Netflix", ServiceNetflix, true},
		{"prime video by id", 9, "Amazon Prime Video", ServicePrimeVideo, true},
		{"apple tv by id after rename", 350, "Apple TV", ServiceAppleTVPlus, true},
		{"apple tv by id before rename", 350, "Apple TV Plus", ServiceAppleTVPlus, true},
		{"disney by id", 337, "Disney Plus", ServiceDisneyPlus, true},
		{"hbo max by id", 1899, "HBO Max", ServiceMax, true},
		{"id wins over an unknown name", 8, "Some New Name", ServiceNetflix, true},

		// Plan variants collapse onto the base service.
		{"paramount premium", 2303, "Paramount Plus Premium", ServiceParamountPlus, true},
		{"paramount essential", 2616, "Paramount Plus Essential", ServiceParamountPlus, true},
		{"peacock premium", 386, "Peacock Premium", ServicePeacock, true},
		{"peacock premium plus", 387, "Peacock Premium Plus", ServicePeacock, true},
		{"netflix with ads", 1796, "Netflix Standard with Ads", ServiceNetflix, true},

		// Unknown IDs fall back to the normalised name.
		{"name fallback", 999001, "Netflix", ServiceNetflix, true},
		{"name fallback ignores case and punctuation", 999002, "  DISNEY+ ", ServiceDisneyPlus, true},
		{"name fallback for a variant", 999003, "Paramount+ Essential", ServiceParamountPlus, true},
		{"espn by id", 1768, "ESPN Plus", ServiceESPNPlus, true},
		{"name fallback for an unknown espn id", 999004, "ESPN Plus", ServiceESPNPlus, true},
		{"name fallback for max", 999005, "Max", ServiceMax, true},

		// Resale channels are a different subscription and never map.
		{"amazon channel", 999010, "Paramount+ Amazon Channel", "", false},
		{"amazon channel for max", 999011, "HBO Max Amazon Channel", "", false},
		{"apple tv channel", 999012, "Paramount Plus Apple TV Channel ", "", false},
		{"roku premium channel", 999013, "Starz Roku Premium Channel", "", false},

		// Storefronts are known providers that are not streaming services.
		{"apple tv store", 2, "Apple TV Store", "", false},
		{"amazon video", 10, "Amazon Video", "", false},
		{"storefront id beats a mappable name", 2, "Apple TV", "", false},

		{"unknown provider", 999020, "Kanopy", "", false},
		{"empty name", 999021, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := MapProvider(tt.id, tt.provider)
			if code != tt.wantCode || ok != tt.wantOK {
				t.Errorf("MapProvider(%d, %q) = (%q, %v), want (%q, %v)", tt.id, tt.provider, code, ok, tt.wantCode, tt.wantOK)
			}
		})
	}
}

// Every code the mapping tables can produce must be a service users can pick,
// and every such service must be reachable through at least one table.
func TestMappingTablesMatchCatalog(t *testing.T) {
	reachable := make(map[string]bool)
	for id, code := range providerIDs {
		if code == notAService {
			continue
		}
		if !ServiceCodeSet[code] {
			t.Errorf("provider ID %d maps to %q, which is not in the catalog", id, code)
		}
		reachable[code] = true
	}
	for name, code := range providerNames {
		if !ServiceCodeSet[code] {
			t.Errorf("provider name %q maps to %q, which is not in the catalog", name, code)
		}
		if normalizeProviderName(name) != name {
			t.Errorf("provider name key %q is not in normalised form", name)
		}
		reachable[code] = true
	}
	for _, svc := range ServiceCatalog {
		if !reachable[svc.Code] {
			t.Errorf("service %q cannot be reached from any TMDb provider", svc.Code)
		}
	}
}

func TestMappedByID(t *testing.T) {
	if !MappedByID(8) {
		t.Error("MappedByID(8) = false, want true")
	}
	if MappedByID(2) {
		t.Error("MappedByID(2) = true for a storefront, want false")
	}
	if MappedByID(999001) {
		t.Error("MappedByID(999001) = true for an unknown ID, want false")
	}
}

func TestServiceName(t *testing.T) {
	tests := map[string]string{
		ServiceMax:         "HBO Max",
		ServiceAppleTVPlus: "Apple TV",
		ServiceNetflix:     "Netflix",
		"not_a_service":    "not_a_service",
	}
	for code, want := range tests {
		if got := ServiceName(code); got != want {
			t.Errorf("ServiceName(%q) = %q, want %q", code, got, want)
		}
	}
}
