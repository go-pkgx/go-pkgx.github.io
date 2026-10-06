package main

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// stub is a latest() that answers from a map and records what was asked.
func stub(m map[string]string) func(string) (string, error) {
	return func(tool string) (string, error) {
		v, ok := m[tool]
		if !ok {
			return "", errors.New("no such tool")
		}
		return v, nil
	}
}

func tree(files map[string]string) fs.FS {
	f := fstest.MapFS{}
	for name, body := range files {
		f[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return f
}

func TestRunPassesWhenEveryPinIsCurrent(t *testing.T) {
	root := tree(map[string]string{
		"layouts/index.html": "sh -s -- pkgm v0.1.7\nsh -s -- pkgx v0.1.10\n$env:PKGM_VERSION='v0.1.7'\n",
		"static/install.sh":  "#   … | sh -s -- pkgm v0.1.7\n",
		"static/install.ps1": "  .\\install.ps1 pkgx v0.1.10\n",
	})
	var out strings.Builder
	if err := run(root, stub(map[string]string{"pkgm": "v0.1.7", "pkgx": "v0.1.10"}), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "5 version(s) checked against 2 release(s)") {
		t.Errorf("summary = %q", out.String())
	}
}

// The case this tool exists for: exactly what the site said on 2026-10-04.
func TestRunNamesEveryPinThatFellBehind(t *testing.T) {
	root := tree(map[string]string{
		"layouts/index.html": "sh -s -- pkgm v0.1.4\nsh -s -- pkgx v0.1.5\n",
	})
	err := run(root, stub(map[string]string{"pkgm": "v0.1.7", "pkgx": "v0.1.10"}), &strings.Builder{})
	if err == nil {
		t.Fatal("run: nil; want a failure naming both pins")
	}
	for _, want := range []string{"layouts/index.html:1", "pkgm v0.1.4", "released v0.1.7", "layouts/index.html:2", "pkgx v0.1.5", "released v0.1.10"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%s", want, err)
		}
	}
}

// A version AHEAD of the latest release is not an error: a release can be
// drafted and the page updated in the same change.
func TestRunAcceptsAPinAheadOfTheRelease(t *testing.T) {
	root := tree(map[string]string{"layouts/index.html": "sh -s -- pkgm v0.2.0\n"})
	if err := run(root, stub(map[string]string{"pkgm": "v0.1.7"}), &strings.Builder{}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// A sweep that reads nothing reports "no problems" exactly like one that read
// everything. It must fail instead.
func TestRunFailsWhenItFindsNothing(t *testing.T) {
	err := run(tree(map[string]string{"layouts/index.html": "no versions here\n"}), stub(nil), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "the layout changed") {
		t.Fatalf("run: %v; want the empty-scan failure", err)
	}
}

func TestRunReportsAnUnreachableRelease(t *testing.T) {
	root := tree(map[string]string{"layouts/index.html": "sh -s -- pkgm v0.1.7\n"})
	err := run(root, func(string) (string, error) { return "", errors.New("403 rate limited") }, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "403 rate limited") {
		t.Fatalf("run: %v; want the fetch error named", err)
	}
}

// Semver, not string order: v0.1.10 is NEWER than v0.1.5, and a lexical
// comparison says the opposite.
func TestRunComparesNumericallyNotLexically(t *testing.T) {
	root := tree(map[string]string{"layouts/index.html": "sh -s -- pkgx v0.1.10\n"})
	if err := run(root, stub(map[string]string{"pkgx": "v0.1.5"}), &strings.Builder{}); err != nil {
		t.Fatalf("v0.1.10 reported as behind v0.1.5: %v", err)
	}
}

func TestCollectSkipsAMissingPath(t *testing.T) {
	// `content` is in the scanned set and absent from this tree.
	pins, err := collect(tree(map[string]string{"layouts/index.html": "sh -s -- pkgm v0.1.7\n"}))
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(pins) != 1 {
		t.Fatalf("pins = %d; want 1", len(pins))
	}
}

// ⛔ A call that did not happen must not read as a verdict about a version.
// The lane went red with `GET releases/latest: 403 Forbidden` printed where a
// list of stale pins usually goes, which invites exactly the wrong conclusion:
// that something is out of date, when in fact nothing was compared.
func TestARateLimitSaysNothingWasCompared(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1760000000")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	_, err := latestRelease("pkgm")
	if err == nil {
		t.Fatal("a 403 must still be an error")
	}
	got := err.Error()
	for _, want := range []string{"rate limited", "NO version was compared", "GITHUB_TOKEN"} {
		if !strings.Contains(got, want) {
			t.Errorf("error %q does not say %q", got, want)
		}
	}
	if !strings.Contains(got, "resets at") {
		t.Errorf("error %q does not say when it resets", got)
	}
}

// A 403 that is NOT a rate limit keeps the plain message: inventing a rate
// limit where there is none would be the same mistake in the other direction.
func TestAPlainForbiddenIsNotCalledARateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "57")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	_, err := latestRelease("pkgm")
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error %q calls a plain 403 a rate limit", err)
	}
}

// The token goes in a header and nowhere else. A test is the only place that
// can say so without printing it.
func TestTheTokenTravelsInTheHeaderOnly(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "a-token-that-must-not-be-logged")
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	got, err := latestRelease("pkgm")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.2.3" {
		t.Errorf("tag = %q, want v1.2.3", got)
	}
	if seen != "Bearer a-token-that-must-not-be-logged" {
		t.Errorf("Authorization header = %q", seen)
	}
}

// And with no token in the environment, no Authorization header at all --
// sending an empty Bearer is a 401 waiting to be misread as something else.
func TestNoTokenMeansNoHeader(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	var seen string
	var had bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, had = r.Header.Get("Authorization"), r.Header.Values("Authorization") != nil
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	if _, err := latestRelease("pkgm"); err != nil {
		t.Fatal(err)
	}
	if had || seen != "" {
		t.Errorf("Authorization header sent with no token: %q", seen)
	}
}
