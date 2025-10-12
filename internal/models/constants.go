package models

// Supported service codes (phase 1 subset). Use lowercase stable codes.
const (
	ServiceNetflix       = "netflix"
	ServicePrimeVideo    = "prime_video"
	ServiceHulu          = "hulu"
	ServiceDisneyPlus    = "disney_plus"
	ServiceMax           = "max"
	ServiceParamountPlus = "paramount_plus"
	ServicePeacock       = "peacock"
	ServiceAppleTVPlus   = "apple_tv_plus"
)

// ServiceInfo describes a streaming service with a stable code and display name.
type ServiceInfo struct {
	Code string
	Name string
}

// ServiceCatalog is the single source of truth for supported services and their display names.
var ServiceCatalog = []ServiceInfo{
	{Code: ServiceNetflix, Name: "Netflix"},
	{Code: ServicePrimeVideo, Name: "Prime Video"},
	{Code: ServiceHulu, Name: "Hulu"},
	{Code: ServiceDisneyPlus, Name: "Disney+"},
	{Code: ServiceMax, Name: "Max"},
	{Code: ServiceParamountPlus, Name: "Paramount+"},
	{Code: ServicePeacock, Name: "Peacock"},
	{Code: ServiceAppleTVPlus, Name: "Apple TV+"},
}

// ServiceCodeSet provides quick validation for service codes (keys are lowercase codes).
var ServiceCodeSet = map[string]bool{
	ServiceNetflix:       true,
	ServicePrimeVideo:    true,
	ServiceHulu:          true,
	ServiceDisneyPlus:    true,
	ServiceMax:           true,
	ServiceParamountPlus: true,
	ServicePeacock:       true,
	ServiceAppleTVPlus:   true,
}
