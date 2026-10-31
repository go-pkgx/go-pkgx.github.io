package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestModulesFindsEveryOne is the test that matters: the whole reason this
// command exists is that a hand-named directory stopped being the whole
// answer, so discovery missing one would reinstate the defect silently.
func TestModulesFindsEveryOne(t *testing.T) {
	root := t.TempDir()
	want := []string{".", "tools/one", "tools/two", "tools/two/nested"}
	for _, dir := range want {
		p := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "go.mod"), []byte("module x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := modules(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("modules() = %v, want %v", got, want)
	}
}

// TestModulesPrunesTheTreesThatAreNotSource keeps a vendored or built tree from
// turning one scan into hundreds — and, more to the point, keeps a go.mod in
// testdata (which several tools here legitimately have) from being scanned as
// if it were ours.
func TestModulesPrunesTheTreesThatAreNotSource(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"tools/real", "public/x", "node_modules/y", "tools/real/testdata/probe", ".git/z"} {
		p := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "go.mod"), []byte("module x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := modules(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tools/real"}
	if !slices.Equal(got, want) {
		t.Errorf("modules() = %v, want %v", got, want)
	}
}

// TestModulesOnThisRepository is the control against the real tree: this
// repository HAS more than one module, and that fact is the reason the command
// exists. A discovery that returned only one would pass both tests above and
// still be the bug.
func TestModulesOnThisRepository(t *testing.T) {
	got, err := modules("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 {
		t.Fatalf("found %v in this repository; it has a root module and several tools/*, "+
			"so fewer than three means discovery is not walking it", got)
	}
	for _, want := range []string{".", "tools/versioncheck", "tools/wasmstage", "tools/playground", "tools/vulnmods"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s not discovered; got %v", want, got)
		}
	}
}

// TestBuildablePackagesTellsTheRootFromABrokenModule is the discriminator the
// whole exit status rests on. A module with no buildable packages is SKIPPED
// and a run of only skips still fails; getting this backwards is how a
// repository that stopped building reports a clean bill.
//
// The negative case is this repository's own shape, not an invented one: a
// single .go file behind `//go:build tools`, which exists so `go mod tidy`
// keeps a Hugo module requirement and never builds. An earlier version of this
// command counted .go FILES and called that module scannable.
func TestBuildablePackagesTellsTheRootFromABrokenModule(t *testing.T) {
	for _, c := range []struct {
		name string
		file string
		body string
		want int
	}{
		{
			name: "a tools-tagged file is not a package",
			file: "kit.go",
			body: "//go:build tools\n\npackage site\n",
			want: 0,
		},
		{
			name: "an ordinary package is one",
			file: "main.go",
			body: "package main\n\nfunc main() {}\n",
			want: 1,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.27.3\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, c.file), []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := buildablePackages(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("buildablePackages = %d, want %d", got, c.want)
			}
		})
	}
}

// TestBuildablePackagesFailsRatherThanReportingZero: a directory that is not a
// module at all must be an ERROR. Returning 0 would classify it as SKIPPED,
// and a skip is how this command says "nothing to see here".
func TestBuildablePackagesFailsRatherThanReportingZero(t *testing.T) {
	if _, err := buildablePackages(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("a missing directory returned no error: it would be reported as SKIPPED")
	}
}

func TestFirstLine(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"one", "one"},
		{"one\ntwo", "one"},
		{"\ntwo", ""},
	} {
		if got := firstLine(c.in); got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
