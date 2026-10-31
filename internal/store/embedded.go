package store

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// StartEmbedded runs a local Postgres (binaries downloaded on first run, no root needed)
// whose credentials match databaseURL. It blocks until ctx is done.
func StartEmbedded(ctx context.Context, databaseURL, dataDir string) error {
	stop, err := LaunchEmbedded(databaseURL, dataDir)
	if err != nil {
		return err
	}
	<-ctx.Done()
	return stop()
}

// LaunchEmbedded starts the embedded Postgres and returns a stop function.
func LaunchEmbedded(databaseURL, dataDir string) (func() error, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return nil, err
	}
	port := 5432
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	db := filepath.Base(u.Path)
	abs, _ := filepath.Abs(dataDir)
	cfg := embeddedpostgres.DefaultConfig().
		Username(user).Password(pass).Database(db).
		Port(uint32(port)).
		Version(embeddedpostgres.V16).
		DataPath(filepath.Join(abs, "pg", "data")).
		RuntimePath(filepath.Join(abs, "pg", "runtime")).
		BinariesPath(filepath.Join(abs, "pg", "bin")).
		CachePath(filepath.Join(abs, "pg", "cache")).
		Logger(nil)
	pg := embeddedpostgres.NewDatabase(cfg)
	if err := pg.Start(); err != nil {
		return nil, fmt.Errorf("embedded postgres: %w", err)
	}
	fmt.Printf("embedded postgres listening on :%d (db=%s user=%s)\n", port, db, user)
	return pg.Stop, nil
}
