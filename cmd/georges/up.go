package main

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/web"
)

// runSite is what `georges` with no arguments does: make sure Postgres is up (starting the
// embedded one if nothing answers), migrate, serve the storefront, and open a browser tab.
func runSite(cmd *cobra.Command, args []string) error {
	cfg := mustConfig()
	ctx := cmd.Context()

	st, err := openOrStartDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	addr := ":8080"
	if ln, err := net.Listen("tcp", addr); err != nil {
		fmt.Println("port 8080 busy; the site is probably already running")
		openBrowser("http://localhost:8080")
		return nil
	} else {
		ln.Close()
	}
	url := "http://localhost" + addr
	fmt.Printf("George's storefront on %s  (Ctrl-C to stop)\n", url)
	go func() {
		time.Sleep(700 * time.Millisecond)
		openBrowser(url)
	}()
	return web.Run(ctx, addr, web.New(st, cfg.DataDir))
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	if err := c.Start(); err != nil {
		fmt.Printf("open %s in your browser (%s)\n", url, strings.TrimSpace(err.Error()))
	}
}

var _ = context.Background
