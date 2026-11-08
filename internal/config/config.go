// Package config loads the process configuration from the environment (.env in development).
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	// Env is "dev" or "prod". Dev renders missing renditions on demand and shows error detail.
	Env string `env:"ENV" envDefault:"dev"`
	// Addr is the HTTP listen address.
	Addr string `env:"ADDR" envDefault:":8080"`
	// DatabaseURL is the v2 store. The default keeps the embedded dev server and uses its own database.
	DatabaseURL string `env:"DATABASE_URL" envDefault:"postgres://georges:georges@localhost:5432/georges2?sslmode=disable"`
	// LegacyDatabaseURL is the v1 database the importer reads from.
	LegacyDatabaseURL string `env:"LEGACY_DATABASE_URL" envDefault:"postgres://georges:georges@localhost:5432/georges?sslmode=disable"`
	// DataDir holds the raw corpus (catalog pages, crops, vendor masters) and the derived assets.
	DataDir string `env:"DATA_DIR" envDefault:"./data/raw"`
	// PublicURL is the canonical origin used in sitemaps and Open Graph tags.
	PublicURL string `env:"PUBLIC_URL" envDefault:"http://localhost:8080"`

	SiteBaseURL string `env:"SITE_BASE_URL" envDefault:"https://www.georgesshowroom.com"`
	SiteUser    string `env:"SITE_USER"`
	SitePass    string `env:"SITE_PASS"`
	FTPHost     string `env:"FTP_HOST" envDefault:"ftp.myplumbingshowroom.com"`
	FTPUser     string `env:"FTP_USER"`
	FTPPass     string `env:"FTP_PASS"`
}

// Load reads .env (when present) and the environment.
func Load() (*Config, error) {
	_ = godotenv.Load()
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		return nil, err
	}
	c.DataDir = abs
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) Dev() bool { return c.Env != "prod" }
