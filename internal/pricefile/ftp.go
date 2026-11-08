// Package pricefile pulls the company price file from Unilog's FTP server and loads it into
// items. The feed is a zip named a-<date> holding one Excel sheet (or several, a million rows
// each) with brand, SKU, UPC and pricing columns for every product on the site; it is
// refreshed daily around 4 a.m. Eastern. Credentials live in .env (FTP_USER, FTP_PASS) and
// are printed on the site's /Price-File-Access.HTML admin page.
package pricefile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// Client is one FTP account.
type Client struct {
	host, user, pass string
}

// New validates the credentials. The password is never logged.
func New(host, user, pass string) (*Client, error) {
	if user == "" || pass == "" {
		return nil, errors.New("pricefile: FTP_USER and FTP_PASS must be set in .env (the admin page /Price-File-Access.HTML shows them)")
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
		return nil, fmt.Errorf("pricefile: login as %s: %w", c.user, err)
	}
	return conn, nil
}

// Entry is one remote file.
type Entry struct {
	Name string
	Size uint64
	Time time.Time
	Dir  bool
}

// List returns the entries in a remote directory ("" = login root), newest first.
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
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

// Latest picks the newest price file: a name starting with "a-" (the documented prefix), else
// the newest zip.
func Latest(ents []Entry) (Entry, bool) {
	var zip Entry
	for _, e := range ents {
		if e.Dir {
			continue
		}
		n := strings.ToLower(e.Name)
		if strings.HasPrefix(n, "a-") {
			return e, true
		}
		if zip.Name == "" && strings.HasSuffix(n, ".zip") {
			zip = e
		}
	}
	return zip, zip.Name != ""
}

// Download fetches one remote file into dir (skipping when a same-size copy exists) and
// returns the local path.
func (c *Client) Download(ctx context.Context, remote Entry, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, filepath.Base(remote.Name))
	if st, err := os.Stat(path); err == nil && remote.Size > 0 && uint64(st.Size()) == remote.Size {
		return path, nil
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Quit()
	res, err := conn.Retr(remote.Name)
	if err != nil {
		return "", fmt.Errorf("pricefile: retr %q: %w", remote.Name, err)
	}
	defer res.Close()
	f, err := os.Create(path + ".part")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, res)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path + ".part")
		return "", err
	}
	return path, os.Rename(path+".part", path)
}
