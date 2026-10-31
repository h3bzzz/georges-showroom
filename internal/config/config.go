// Package config loads settings from the environment (and .env when present).
package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	SiteBaseURL string `env:"SITE_BASE_URL" envDefault:"https://www.georgesshowroom.com"`
	SiteUser    string `env:"SITE_USER"`
	SitePass    string `env:"SITE_PASS"`

	FTPHost string `env:"FTP_HOST" envDefault:"ftp.myplumbingshowroom.com"`
	FTPUser string `env:"FTP_USER"`
	FTPPass string `env:"FTP_PASS"`

	DatabaseURL string `env:"DATABASE_URL"`
	DataDir     string `env:"DATA_DIR" envDefault:"./data/raw"`

	ChromeBin        string `env:"CHROME_BIN" envDefault:"/usr/bin/google-chrome-stable"`
	ChromeProfileDir string `env:"CHROME_PROFILE_DIR" envDefault:"./.chrome-profile"`
	ChromeHeadless   bool   `env:"CHROME_HEADLESS" envDefault:"false"`
	ChromeControlURL string `env:"CHROME_CONTROL_URL"` // e.g. http://127.0.0.1:9222 to attach to a running Chrome

	BraveAPIKey string `env:"BRAVE_API_KEY"` // Brave Search API, used by `webimages` to find product pages on brand sites
}

// Load reads .env (if it exists) then the process environment.
func Load() (*Config, error) {
	_ = godotenv.Load() // missing .env is fine
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}
