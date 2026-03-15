package main

import "time"

const (
	MaxBodyBytes           = 10 << 20 // 10 MiB
	ReadTimeout            = 15 * time.Second
	ReadHeaderTimeout      = 5 * time.Second
	IdleTimeout            = 60 * time.Second
	AuthRateLimitBurst     = 20
	UserRateLimitBurst     = 60
	AllowedDomain          = "myowndomain.com"
	OAuthStateCookieMaxAge = 300   // 5 minutes
	SessionCookieMaxAge    = 86400 // 24 hours
	SessionTTL             = 24 * time.Hour
	TokenRefreshThreshold  = 5 * time.Minute
	SessionCollection      = "sessions"
)
