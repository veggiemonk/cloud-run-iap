package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ardanlabs/conf/v3"
)

// Constants that should NOT be configurable (security/protocol values).
const (
	MaxBodyBytes           = 10 << 20 // 10 MiB
	ReadTimeout            = 15 * time.Second
	ReadHeaderTimeout      = 5 * time.Second
	IdleTimeout            = 60 * time.Second
	OAuthStateCookieMaxAge = 300   // 5 minutes
	SessionCookieMaxAge    = 86400 // 24 hours
	SessionTTL             = 24 * time.Hour
	TokenRefreshThreshold  = 5 * time.Minute
)

// Config holds all application configuration from environment variables.
type Config struct {
	ProjectID            string `conf:"env:PROJECT_ID,required"`
	Port                 string `conf:"env:PORT,default:8080"`
	FirestoreDB          string `conf:"env:FIRESTORE_DATABASE,default:(default)"`
	SessionEncryptionKey string `conf:"env:SESSION_ENCRYPTION_KEY,required,mask"`
	CSRFKey              string `conf:"env:CSRF_KEY,mask"`
	KRevision            string `conf:"env:K_REVISION"`
	AllowedDomain        string `conf:"env:ALLOWED_DOMAIN,default:myowndomain.com"`
	AuthRateLimitBurst   int    `conf:"env:AUTH_RATE_LIMIT_BURST,default:20"`
	UserRateLimitBurst   int    `conf:"env:USER_RATE_LIMIT_BURST,default:60"`
	Google               struct {
		OAuthConfig  string `conf:"env:GOOGLE_OAUTH_CONFIG,mask"`
		ClientID     string `conf:"env:GOOGLE_CLIENT_ID"`
		ClientSecret string `conf:"env:GOOGLE_CLIENT_SECRET,mask"`
		RedirectURL  string `conf:"env:OAUTH_REDIRECT_URL,default:http://localhost:8080/auth/callback"`
	}
}

// MustParse parses configuration from environment variables and exits on failure.
func MustParse() Config {
	var cfg Config
	help, err := conf.Parse("", &cfg)
	if err != nil {
		if errors.Is(err, conf.ErrHelpWanted) {
			fmt.Println(help)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "parsing config: %v\n", err)
		os.Exit(1)
	}
	if cfgStr, err := conf.String(&cfg); err == nil {
		slog.Info("startup", "config", cfgStr)
	}
	return cfg
}
