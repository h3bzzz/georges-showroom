// Package pricefile pulls the company-specific daily price file from Unilog's FTP server.
// The feed carries brand, SKU, UPC and pricing for every published product, split into files
// of at most one million rows.
package pricefile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jlaffaye/ftp"
)

type Client struct {
	host, user, pass string
}

func New(host, user, pass string) (*Client, error) {
	if user == "" || pass == "" {
		return nil, errors.New("pricefile: FTP_USER and FTP_PASS must be set in .env (see /Price-File-Access.HTML)")
	}
	if host == "" {
		host = "ftp.myplumbingshowroom.com"
	}
	return &Client{host: host, user: user, pass: pass}, nil
}

func (c *Client) dial(ctx context.Context) (*ftp.ServerConn, error) {
	conn, err := ftp.Dial(c.host+":21", ftp.DialWithContext(ctx), ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return nil, fmt.Errorf("pricefile: dial %s: %w", c.host, err)
	}
	if err := conn.Login(c.user, c.pass); err != nil {
		conn.Quit()
		return nil, fmt.Errorf("pricefile: login: %w", err)
	}
	return conn, nil
}

type Entry struct {
	Name string
	Size uint64
	Time time.Time
	Dir  bool
}

// List returns the entries in the given remote directory ("" = login root).
func (c *Client) List(ctx context.Context, dir string) ([]Entry, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Quit()
	ents, err := conn.List(dir)
	if err != nil {
		return nil, fmt.Errorf("pricefile: list %q: %w", dir, err)
	}
	out := make([]Entry, 0, len(ents))
	for _, e := range ents {
		out = append(out, Entry{Name: e.Name, Size: e.Size, Time: e.Time, Dir: e.Type == ftp.EntryTypeFolder})
	}
	return out, nil
}

// Download fetches one remote file into dir, returning the local path and byte count.
func (c *Client) Download(ctx context.Context, remote, dir string) (string, int64, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return "", 0, err
	}
	defer conn.Quit()
	res, err := conn.Retr(remote)
	if err != nil {
		return "", 0, fmt.Errorf("pricefile: retr %q: %w", remote, err)
	}
	defer res.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	path := filepath.Join(dir, filepath.Base(remote))
	f, err := os.Create(path + ".part")
	if err != nil {
		return "", 0, err
	}
	n, err := io.Copy(f, res)
	f.Close()
	if err != nil {
		return "", n, err
	}
	return path, n, os.Rename(path+".part", path)
}
