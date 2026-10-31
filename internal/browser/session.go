// Package browser drives the locally installed Chrome through go-rod with a persistent profile,
// which is the only reliable way through Cloudflare's managed challenge on georgesshowroom.com.
// The first run is interactive: log in once (and clear the challenge); later runs reuse cookies.
package browser

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-rod/rod/lib/proto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/stealth"
)

type Options struct {
	ControlURL string // when set, attach to an already running Chrome (--remote-debugging-port)
	ChromeBin  string
	ProfileDir string
	Headless   bool
	BaseURL    string
	User, Pass string
}

type Session struct {
	opts    Options
	browser *rod.Browser
	l       *launcher.Launcher
}

func Open(ctx context.Context, o Options) (*Session, error) {
	if o.ControlURL != "" {
		u, err := launcher.ResolveURL(o.ControlURL)
		if err != nil {
			return nil, fmt.Errorf("browser: attach %s: %w (start Chrome with --remote-debugging-port=9222)", o.ControlURL, err)
		}
		b := rod.New().ControlURL(u).Context(ctx)
		if err := b.Connect(); err != nil {
			return nil, fmt.Errorf("browser: attach: %w", err)
		}
		return &Session{opts: o, browser: b}, nil
	}
	abs, err := filepath.Abs(o.ProfileDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	l := launcher.New().
		Bin(o.ChromeBin).
		UserDataDir(abs).
		Headless(o.Headless).
		Leakless(false).
		Set("disable-blink-features", "AutomationControlled").
		Set("window-size", "1400,1000")
	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("browser: launch: %w", err)
	}
	b := rod.New().ControlURL(u).Context(ctx)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("browser: connect: %w", err)
	}
	return &Session{opts: o, browser: b, l: l}, nil
}

func (s *Session) Close() {
	if s.opts.ControlURL != "" {
		return // attached to the user's own Chrome: leave it running
	}
	if s.browser != nil {
		_ = s.browser.Close()
	}
	if s.l != nil {
		s.l.Cleanup()
	}
}

// Page opens a new stealth page.
func (s *Session) Page() (*rod.Page, error) {
	if s.opts.ControlURL != "" {
		return s.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	}
	return stealth.Page(s.browser)
}

// isChallenge reports whether the current document is Cloudflare's interstitial.
func isChallenge(p *rod.Page) bool {
	title, _ := p.Eval(`() => document.title`)
	t := title.Value.Str()
	return strings.Contains(t, "Just a moment") || strings.Contains(t, "Attention Required")
}

// Navigate loads url and waits until the Cloudflare challenge (if shown) has cleared.
func (s *Session) Navigate(p *rod.Page, url string, wait time.Duration) error {
	if err := p.Navigate(url); err != nil {
		return err
	}
	if err := p.WaitLoad(); err != nil {
		return err
	}
	deadline := time.Now().Add(wait)
	for isChallenge(p) {
		if time.Now().After(deadline) {
			return errors.New("browser: Cloudflare challenge did not clear; run `georges browser login` headed and solve it once")
		}
		time.Sleep(1500 * time.Millisecond)
	}
	// Unilog's own bot gate redirects to /productImages/assets/iAmHuman.html?X-REWRITE-URL=…;
	// re-requesting the page with iAmHuman=true satisfies it.
	if info, err := p.Info(); err == nil && strings.Contains(info.URL, "iAmHuman.html") {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		if err := p.Navigate(url + sep + "iAmHuman=true"); err != nil {
			return err
		}
		if err := p.WaitLoad(); err != nil {
			return err
		}
	}
	return nil
}

// HTML returns the current document's outerHTML.
func HTML(p *rod.Page) (string, error) {
	return p.HTML()
}

// Text returns document.body.innerText (rendered text) — handy for sitemaps and XML.
func Text(p *rod.Page) (string, error) {
	v, err := p.Eval(`() => document.body ? document.body.innerText : ''`)
	if err != nil {
		return "", err
	}
	return v.Value.Str(), nil
}

// LoggedIn checks the header for the "Logout" link the site shows to authenticated users.
func LoggedIn(p *rod.Page) bool {
	v, err := p.Eval(`() => !!document.querySelector('a[href="/logout.HTML"]')`)
	return err == nil && v.Value.Bool()
}

// Login submits the site login form when not already logged in.
func (s *Session) Login(p *rod.Page) error {
	if err := s.Navigate(p, s.opts.BaseURL+"/login.HTML", 90*time.Second); err != nil {
		return err
	}
	if LoggedIn(p) {
		return nil
	}
	if s.opts.User == "" || s.opts.Pass == "" {
		return errors.New("browser: SITE_USER / SITE_PASS not set")
	}
	user, err := p.Timeout(15 * time.Second).Element(`input[type="email"], input[name*="mail" i], input[name*="user" i]`)
	if err != nil {
		return fmt.Errorf("browser: login form not found: %w", err)
	}
	if err := user.Input(s.opts.User); err != nil {
		return err
	}
	pass, err := p.Element(`input[type="password"]`)
	if err != nil {
		return err
	}
	if err := pass.Input(s.opts.Pass); err != nil {
		return err
	}
	if err := pass.Type(input.Enter); err != nil { // Enter submits the form
		return err
	}
	if err := p.WaitLoad(); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	if !LoggedIn(p) {
		return errors.New("browser: login did not succeed (check credentials or solve a challenge in the window)")
	}
	return nil
}

// FetchImage loads an image URL in the page (so Cloudflare cookies apply) and returns its bytes.
func (s *Session) FetchImage(p *rod.Page, url string) ([]byte, error) {
	if err := s.Navigate(p, url, 30*time.Second); err != nil {
		return nil, err
	}
	b, err := p.GetResource(url)
	if err != nil {
		return nil, err
	}
	if len(b) < 100 || strings.Contains(strings.ToLower(string(b[:min(len(b), 200)])), "<html") {
		return nil, errors.New("browser: not an image (blocked?)")
	}
	return b, nil
}
