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

// ServiceCatalog is the single source of truth for supported services and their display names.
var ServiceCatalog = []ServiceInfo{
	{Code: ServiceNetflix, Name: "Netflix", Access: ServiceAccessSubscription},
	{Code: ServicePrimeVideo, Name: "Prime Video", Access: ServiceAccessSubscription},
	{Code: ServiceHulu, Name: "Hulu", Access: ServiceAccessSubscription},
	{Code: ServiceDisneyPlus, Name: "Disney+", Access: ServiceAccessSubscription},
	{Code: ServiceMax, Name: "Max", Access: ServiceAccessSubscription},
	{Code: ServiceParamountPlus, Name: "Paramount+", Access: ServiceAccessSubscription},
	{Code: ServicePeacock, Name: "Peacock", Access: ServiceAccessSubscription},
	{Code: ServiceAppleTVPlus, Name: "Apple TV+", Access: ServiceAccessSubscription},
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
var ServiceCodeSet = map[string]bool{
	ServiceNetflix:       true,
	ServicePrimeVideo:    true,
	ServiceHulu:          true,
	ServiceDisneyPlus:    true,
	ServiceMax:           true,
	ServiceParamountPlus: true,
	ServicePeacock:       true,
	ServiceAppleTVPlus:   true,
	ServicePlutoTV:       true,
	ServiceTubi:          true,
	ServiceRokuChannel:   true,
	ServicePlex:          true,
	ServiceMGMPlus:       true,
	ServiceCrunchyroll:   true,
	ServiceStarz:         true,
	ServiceAMCPlus:       true,
	ServiceESPNPlus:      true,
	ServiceDiscoveryPlus: true,
}
