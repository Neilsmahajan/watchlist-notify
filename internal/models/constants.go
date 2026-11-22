package models

// Supported service codes. These are stored on user documents, so they are a
// stable contract: display names and TMDb provider IDs may change, codes never do.
const (
	ServiceNetflix       = "netflix"
	ServicePrimeVideo    = "prime_video"
	ServiceHulu          = "hulu"
	ServiceDisneyPlus    = "disney_plus"
	ServiceMax           = "max"
	ServiceParamountPlus = "paramount_plus"
	ServicePeacock       = "peacock"
	ServiceAppleTVPlus   = "apple_tv_plus"
	ServicePlutoTV       = "pluto_tv"
	ServiceTubi          = "tubi"
	ServiceRokuChannel   = "roku_channel"
	ServicePlex          = "plex"
	ServiceMGMPlus       = "mgm_plus"
	ServiceCrunchyroll   = "crunchyroll"
	ServiceStarz         = "starz"
	ServiceAMCPlus       = "amc_plus"
	ServiceESPNPlus      = "espn_plus"
	ServiceDiscoveryPlus = "discovery_plus"
)

// ServiceAccess categorizes how a service is typically accessed by users.
const (
	ServiceAccessSubscription = "subscription"
	ServiceAccessFree         = "free"
	ServiceAccessAds          = "ads"
)

// ServiceInfo describes a streaming service with a stable code and display attributes.
type ServiceInfo struct {
	Code   string
	Name   string
	Access string
}

// ServiceCatalog is the single source of truth for supported services and
// their display names. Names follow what the services call themselves today
// (TMDb renamed Max back to "HBO Max" and Apple TV+ to "Apple TV").
var ServiceCatalog = []ServiceInfo{
	{Code: ServiceNetflix, Name: "Netflix", Access: ServiceAccessSubscription},
	{Code: ServicePrimeVideo, Name: "Prime Video", Access: ServiceAccessSubscription},
	{Code: ServiceHulu, Name: "Hulu", Access: ServiceAccessSubscription},
	{Code: ServiceDisneyPlus, Name: "Disney+", Access: ServiceAccessSubscription},
	{Code: ServiceMax, Name: "HBO Max", Access: ServiceAccessSubscription},
	{Code: ServiceParamountPlus, Name: "Paramount+", Access: ServiceAccessSubscription},
	{Code: ServicePeacock, Name: "Peacock", Access: ServiceAccessSubscription},
	{Code: ServiceAppleTVPlus, Name: "Apple TV", Access: ServiceAccessSubscription},
	{Code: ServicePlutoTV, Name: "Pluto TV", Access: ServiceAccessAds},
	{Code: ServiceTubi, Name: "Tubi", Access: ServiceAccessAds},
	{Code: ServiceRokuChannel, Name: "The Roku Channel", Access: ServiceAccessAds},
	{Code: ServicePlex, Name: "Plex", Access: ServiceAccessAds},
	{Code: ServiceMGMPlus, Name: "MGM+", Access: ServiceAccessSubscription},
	{Code: ServiceCrunchyroll, Name: "Crunchyroll", Access: ServiceAccessSubscription},
	{Code: ServiceStarz, Name: "Starz", Access: ServiceAccessSubscription},
	{Code: ServiceAMCPlus, Name: "AMC+", Access: ServiceAccessSubscription},
	{Code: ServiceESPNPlus, Name: "ESPN+", Access: ServiceAccessSubscription},
	{Code: ServiceDiscoveryPlus, Name: "Discovery+", Access: ServiceAccessSubscription},
}

// ServiceCodeSet provides quick validation for service codes (keys are lowercase codes).
var ServiceCodeSet = func() map[string]bool {
	set := make(map[string]bool, len(ServiceCatalog))
	for _, s := range ServiceCatalog {
		set[s.Code] = true
	}
	return set
}()

var serviceNames = func() map[string]string {
	names := make(map[string]string, len(ServiceCatalog))
	for _, s := range ServiceCatalog {
		names[s.Code] = s.Name
	}
	return names
}()

// ServiceName returns the display name for a service code, or the code itself
// when it is not in the catalog.
func ServiceName(code string) string {
	if name, ok := serviceNames[code]; ok {
		return name
	}
	return code
}

// DefaultRegion is used when neither the user nor the environment names one.
const DefaultRegion = "US"
