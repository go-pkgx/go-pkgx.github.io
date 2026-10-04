package main

import (
	"errors"
	"io/fs"
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
		"layouts/index.html":  "sh -s -- pkgm v0.1.7\nsh -s -- pkgx v0.1.10\n$env:PKGM_VERSION='v0.1.7'\n",
		"static/install.sh":   "#   … | sh -s -- pkgm v0.1.7\n",
		"static/install.ps1":  "  .\\install.ps1 pkgx v0.1.10\n",
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
