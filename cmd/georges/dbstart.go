package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"georges_/internal/config"
	"georges_/internal/store"
)

// openOrStartDB connects to Postgres; when nothing answers it launches the embedded server as
// a detached `georges db start` process (own session, log in DATA_DIR/pg.log) and waits for
// it. Detaching matters under air: the serve process is killed on every rebuild, and the
// database must outlive it so pipelines and the next serve keep their connection.
func openOrStartDB(ctx context.Context, cfg *config.Config) (*store.Store, error) {
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err == nil {
		return st, nil
	}
	fmt.Println("no Postgres answering; starting the embedded one in the background…")
	if err := spawnDBStart(cfg.DataDir); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if st, err = store.Open(ctx, cfg.DatabaseURL); err == nil {
			return st, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("embedded postgres did not come up: %w (see %s)", err, filepath.Join(cfg.DataDir, "pg.log"))
}

func spawnDBStart(dataDir string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(dataDir, "pg.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	c := exec.Command(self, "db", "start")
	c.Stdout, c.Stderr = logf, logf
	c.Stdin = nil
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survive our SIGINT/SIGTERM from air
	if err := c.Start(); err != nil {
		return fmt.Errorf("spawn db start: %w", err)
	}
	return c.Process.Release()
}
