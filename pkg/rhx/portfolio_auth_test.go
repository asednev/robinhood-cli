package rhx

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPortfolioValidSessionUsesAccountProbeWithoutRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/accounts/" || r.URL.Query().Get("default_to_all_accounts") != "true" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Errorf("unexpected authentication probe: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `{"results":[],"next":null}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	path := sessionPath(dir, "default")
	if err := saveSession(path, Session{AccessToken: "fixture", RefreshToken: "unused"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewPortfolioConnection("default", dir)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(server.URL)
	c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
	if err := c.RefreshSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	last, err := os.Stat(path)
	if err != nil || !bytes.Equal(before, after) || !os.SameFile(info, last) || !info.ModTime().Equal(last.ModTime()) {
		t.Fatal("valid session was modified")
	}
}

func TestPortfolioConnectionConcurrentRefresh(t *testing.T) {
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/accounts/":
			if r.Header.Get("Authorization") != "Bearer new" {
				w.WriteHeader(401)
				fmt.Fprint(w, `{"detail":"secret"}`)
				return
			}
			fmt.Fprint(w, `{"results":[]}`)
		case "/oauth2/token/":
			r.ParseForm()
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
				t.Error("unexpected authentication request")
				w.WriteHeader(400)
				return
			}
			refreshes.Add(1)
			fmt.Fprint(w, `{"access_token":"new","refresh_token":"new-refresh"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := saveSession(sessionPath(dir, "shared"), Session{AccessToken: "old", RefreshToken: "old-refresh"}); err != nil {
		t.Fatal(err)
	}
	makeConn := func(profile string) *PortfolioConnection {
		c, err := NewPortfolioConnection(profile, dir)
		if err != nil {
			t.Fatal(err)
		}
		target, _ := url.Parse(server.URL)
		c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
		return c
	}
	a, b := makeConn("shared"), makeConn("shared")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, c := range []*PortfolioConnection{a, b} {
		wg.Add(1)
		go func(c *PortfolioConnection) { defer wg.Done(); errs <- c.RefreshSession(context.Background()) }(c)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes = %d", refreshes.Load())
	}
	saved, err := loadSession(sessionPath(dir, "shared"))
	if err != nil || saved.RefreshToken != "new-refresh" {
		t.Fatal("rotated session not persisted")
	}
	for _, path := range []string{sessionPath(dir, "shared"), sessionPath(dir, "shared") + ".lock"} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("insecure mode on %s", filepath.Base(path))
		}
	}
	if err := makeConn("other").RefreshSession(context.Background()); cliError(err).Code != ErrorAuthRequired {
		t.Fatalf("other profile: %v", err)
	}
}

func TestPortfolioRefreshCrossProcess(t *testing.T) {
	if addr := os.Getenv("RHX_FIXTURE_SERVER"); addr != "" {
		c, err := NewPortfolioConnection("shared", os.Getenv("RHX_FIXTURE_DIR"))
		if err != nil {
			t.Fatal(err)
		}
		target, err := url.Parse(addr)
		if err != nil {
			t.Fatal(err)
		}
		c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
		if err := c.RefreshSession(context.Background()); err != nil {
			t.Fatal(err)
		}
		return
	}
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/accounts/":
			if r.Header.Get("Authorization") != "Bearer rotated" {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"results":[]}`)
		case "/oauth2/token/":
			if err := r.ParseForm(); err != nil || r.Form.Get("refresh_token") != "old" {
				w.WriteHeader(401)
				return
			}
			refreshes.Add(1)
			time.Sleep(75 * time.Millisecond)
			fmt.Fprint(w, `{"access_token":"rotated","refresh_token":"new"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := saveSession(sessionPath(dir, "shared"), Session{AccessToken: "expired", RefreshToken: "old"}); err != nil {
		t.Fatal(err)
	}
	commands := make([]*exec.Cmd, 2)
	for i := range commands {
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestPortfolioRefreshCrossProcess$")
		commands[i].Env = append(os.Environ(), "RHX_FIXTURE_SERVER="+server.URL, "RHX_FIXTURE_DIR="+dir)
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refresh count = %d", refreshes.Load())
	}
}

func TestPortfolioLoginInjectionAndSanitizedErrors(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/oauth2/token/" {
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		r.ParseForm()
		if r.Form.Get("username") != "fixture-user" || r.Form.Get("password") != "fixture-password" {
			t.Error("injected credentials missing")
		}
		w.WriteHeader(403)
		fmt.Fprint(w, `{"detail":"fixture-password private-account"}`)
	}))
	defer server.Close()
	c, err := NewPortfolioConnection("default", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(server.URL)
	c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
	err = c.Login(context.Background(), PortfolioCredentials{Username: "fixture-user", Password: "fixture-password"}, false)
	if err == nil || strings.Contains(err.Error(), "fixture-password") || strings.Contains(err.Error(), "private-account") {
		t.Fatalf("unsafe error: %v", err)
	}
	if requests.Load() != 1 {
		t.Fatal("unexpected request count")
	}
	if _, err := os.Stat(c.auth.SessionPath); !os.IsNotExist(err) {
		t.Fatal("failed login saved a session")
	}
	for _, profile := range []string{"../bad", "a/b", ".", "a b"} {
		if _, err := NewPortfolioConnection(profile, t.TempDir()); err == nil {
			t.Errorf("accepted profile %q", profile)
		}
	}
}

func TestPortfolioLockHonorsCancellation(t *testing.T) {
	c, err := NewPortfolioConnection("default", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- c.withSessionLock(context.Background(), func() error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = c.withSessionLock(ctx, func() error { t.Error("acquired locked session"); return nil })
	close(release)
	<-done
	if err != context.DeadlineExceeded {
		t.Fatalf("got %v", err)
	}
}

func TestPortfolioRefreshChallengeAndRevocationNeverLogIn(t *testing.T) {
	for _, mode := range []string{"challenge", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/accounts/":
					w.WriteHeader(http.StatusUnauthorized)
				case "/oauth2/token/":
					calls.Add(1)
					r.ParseForm()
					if r.Form.Get("grant_type") != "refresh_token" {
						t.Error("password login attempted")
					}
					w.WriteHeader(http.StatusForbidden)
					if mode == "challenge" {
						fmt.Fprint(w, `{"verification_workflow":{"id":"sensitive"}}`)
					} else {
						fmt.Fprint(w, `{"detail":"private revoked token"}`)
					}
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			if err := saveSession(sessionPath(dir, "default"), Session{AccessToken: "expired", RefreshToken: "refresh"}); err != nil {
				t.Fatal(err)
			}
			c, _ := NewPortfolioConnection("default", dir)
			target, _ := url.Parse(server.URL)
			c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
			err := c.RefreshSession(context.Background())
			if err == nil || cliError(err).Code != ErrorAuthRequired || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "private") {
				t.Fatalf("unexpected error %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("refresh calls %d", calls.Load())
			}
			saved, err := loadSession(c.auth.SessionPath)
			if err != nil || saved.RefreshToken != "refresh" {
				t.Fatal("failed refresh changed stored session")
			}
		})
	}
}

func TestPortfolioRejectsAuthenticationRedirect(t *testing.T) {
	var leaked atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, foreign.URL, http.StatusFound) }))
	defer server.Close()
	dir := t.TempDir()
	if err := saveSession(sessionPath(dir, "default"), Session{AccessToken: "fixture", RefreshToken: "fixture-refresh"}); err != nil {
		t.Fatal(err)
	}
	c, _ := NewPortfolioConnection("default", dir)
	target, _ := url.Parse(server.URL)
	c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
	if err := c.RefreshSession(context.Background()); err == nil {
		t.Fatal("redirect accepted as authenticated")
	}
	if leaked.Load() != 0 {
		t.Fatal("credential sent to redirect target")
	}
	if _, err := sessionFromTokenPayload(map[string]any{"access_token": "fixture"}, http.StatusFound, "", ""); err == nil {
		t.Fatal("redirect accepted as token")
	}
}
