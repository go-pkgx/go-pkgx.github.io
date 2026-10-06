package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The check knew the file, the line, the tool and both versions. The
// rewriting was done by hand — three times in one evening, with a
// throwaway script and a regex over the whole file. Two descriptions of
// one rule, and the ad-hoc one is the one nobody tests.
//
// It cost what an untested rewriter costs: one of those edits matched no
// text at all, said nothing, and the commit went out anyway.

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFixRewritesOnlyTheLinesThatAreBehind(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"layouts/index.html": "install: pkgx v0.9.0 here\n" +
			"and PKGM_VERSION='v0.2.2' there\n" +
			"a sentence about pkgx v0.1.0 as it was in July\n",
		"static/install.sh": "# curl … | sh -s -- pkgm v0.2.2\n",
	})
	behind := []pin{
		{file: "layouts/index.html", line: 1, tool: "pkgx", ver: "v0.9.0"},
		{file: "layouts/index.html", line: 2, tool: "pkgm", ver: "v0.2.2"},
		{file: "static/install.sh", line: 1, tool: "pkgm", ver: "v0.2.2"},
	}
	current := map[string]string{"pkgx": "v0.10.2", "pkgm": "v0.2.3"}

	var out bytes.Buffer
	if err := fixPins(dir, behind, current, &out); err != nil {
		t.Fatalf("fixPins: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "layouts/index.html"))
	lines := strings.Split(string(got), "\n")
	if !strings.Contains(lines[0], "v0.10.2") {
		t.Errorf("line 1 not rewritten: %q", lines[0])
	}
	if !strings.Contains(lines[1], "v0.2.3") {
		t.Errorf("line 2 not rewritten: %q", lines[1])
	}
	// THE LINE THE SCAN DID NOT REPORT IS UNTOUCHED. A regex over the
	// whole file — which is what the hand-rolled script did — would have
	// rewritten a sentence that was describing history.
	if !strings.Contains(lines[2], "v0.1.0") {
		t.Errorf("a line nobody reported was rewritten: %q", lines[2])
	}

	sh, _ := os.ReadFile(filepath.Join(dir, "static/install.sh"))
	if !strings.Contains(string(sh), "v0.2.3") {
		t.Errorf("install.sh not rewritten: %q", sh)
	}
}

// A line the scan called behind and the rewrite could not change is a
// DISAGREEMENT between the two halves of this program. The dangerous
// outcome is reporting success over it — which is how an edit that matched
// nothing shipped in the first place.
func TestFixRefusesWhenItCannotRewriteAReportedLine(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"layouts/index.html": "a line with no version in it at all\n",
	})
	err := fixPins(dir,
		[]pin{{file: "layouts/index.html", line: 1, tool: "pkgx", ver: "v0.9.0"}},
		map[string]string{"pkgx": "v0.10.2"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("a line it could not rewrite was reported as fixed")
	}
	if !strings.Contains(err.Error(), "disagree") {
		t.Errorf("err = %v", err)
	}
}

// It must not invent a version for a tool it was given no release for.
func TestFixLeavesAnUnknownToolAlone(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"layouts/index.html": "install: mirror v0.3.0\n",
	})
	err := fixPins(dir,
		[]pin{{file: "layouts/index.html", line: 1, tool: "mirror", ver: "v0.3.0"}},
		map[string]string{"pkgx": "v0.10.2"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("a tool with no known release was silently left behind and called fixed")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "layouts/index.html"))
	if !strings.Contains(string(got), "v0.3.0") {
		t.Errorf("it wrote something: %q", got)
	}
}

// And the whole point: after the rewrite, the ORIGINAL check must pass
// against the bytes on disk. A fixer that reports success on its own say-so
// is the failure this file is about.
func TestFixThenTheCheckPasses(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"layouts/index.html": "install: pkgx v0.9.0 and PKGM_VERSION='v0.2.2'\n",
	})
	latest := func(tool string) (string, error) {
		return map[string]string{"pkgx": "v0.10.2", "pkgm": "v0.2.3"}[tool], nil
	}

	// Before: the check fails.
	if err := run(os.DirFS(dir), latest, &bytes.Buffer{}); err == nil {
		t.Fatal("the check passed on a stale tree — this test proves nothing")
	}

	pins, err := collect(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	current := map[string]string{"pkgx": "v0.10.2", "pkgm": "v0.2.3"}
	if err := fixPins(dir, pins, current, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	// After: it passes, read back off the disk.
	if err := run(os.DirFS(dir), latest, &bytes.Buffer{}); err != nil {
		t.Errorf("the check still fails after the fix: %v", err)
	}
}

// AND THE PROPERTY THAT MATTERS MOST: the fixer re-runs the ORIGINAL check
// against the bytes it just wrote.
//
// This test exists because `mutate` showed the one above could not see it:
// deleting the final re-check left the suite green, since that test called
// fixPins and run by hand instead of going through fixAndVerify. The whole
// claim of this tool is "it does not report success on its own say-so",
// and nothing was holding it.
func TestFixAndVerifyRunsTheCheckAfterwards(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"layouts/index.html": "install: pkgx v0.9.0 here\n",
	})
	calls := 0
	latest := func(tool string) (string, error) {
		calls++
		return map[string]string{"pkgx": "v0.10.2"}[tool], nil
	}
	var out bytes.Buffer
	if err := fixAndVerify(dir, latest, &out); err != nil {
		t.Fatalf("fixAndVerify: %v", err)
	}
	// It asked twice: once to decide what is behind, once to CHECK.
	if calls < 2 {
		t.Errorf("the release was looked up %d time(s) — it did not check afterwards", calls)
	}
	if !strings.Contains(out.String(), "version(s) checked against") {
		t.Errorf("the check's own report is absent: %q", out.String())
	}

	// A fixer that writes nothing and reports success is the failure this
	// is about: the file really changed.
	got, _ := os.ReadFile(filepath.Join(dir, "layouts/index.html"))
	if !strings.Contains(string(got), "v0.10.2") {
		t.Errorf("nothing was written: %q", got)
	}
}
