package db

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// LaunchEmbedded starts the development Postgres (binaries downloaded once under DATA_DIR/pg,
// no root) with the credentials and port of dsn, and returns a stop function. The v1 and v2
// databases share this one server; EnsureDatabase creates the v2 database on first use.
func LaunchEmbedded(dsn, dataDir string) (func() error, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}
	port := 5432
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	abs, _ := filepath.Abs(dataDir)
	cfg := embeddedpostgres.DefaultConfig().
		Username(user).Password(pass).Database("georges").
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
	return pg.Stop, nil
}
