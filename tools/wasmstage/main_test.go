package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pkgx/bottle"
)

// wasmModule is the smallest thing debug-free tooling and a browser both
// accept as WebAssembly: the magic number and the version word.
var wasmModule = []byte{0x00, 'a', 's', 'm', 0x01, 0x00, 0x00, 0x00}

func write(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// withSeams replaces the pick, the install and the temp dir, so the command
// runs with no network. Both the pick and the install assert the platform,
// because staging a host-architecture bottle into a web page is the one
// mistake that would look fine until somebody opened it.
func withSeams(t *testing.T, err error, stage func(dir string)) {
	t.Helper()
	pp, pi, pt := stagePick, stageInstall, stageTempDir
	stagePick = func(project, constraint, osn, arch string) (bottle.Ver, error) {
		if osn != "wasip1" || arch != "wasm" {
			t.Errorf("picked for %s/%s, want wasip1/wasm", osn, arch)
		}
		if err != nil {
			return bottle.Ver{}, err
		}
		return bottle.ParseVer("1.0.0"), nil
	}
	stageInstall = func(_ bottle.Resolved, dir, osn, arch string) (bool, error) {
		if osn != "wasip1" || arch != "wasm" {
			t.Errorf("installed for %s/%s, want wasip1/wasm", osn, arch)
		}
		if stage != nil {
			stage(dir)
		}
		return true, nil
	}
	t.Cleanup(func() { stagePick, stageInstall, stageTempDir = pp, pi, pt })
}

func drive(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func readManifest(t *testing.T, dir string) Manifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// The shape it exists for: the seven projects the wasip1 lanes publish, here
// reduced to two, staged out of a bottle store into files a page can fetch.
func TestItStagesTheModulesAndWritesAManifest(t *testing.T) {
	out := t.TempDir()
	store := t.TempDir()
	withSeams(t, nil, nil)
	write(t, filepath.Join(store, "sqlite.org/v3.53.4/bin/sqlite3.wasm"), wasmModule)
	write(t, filepath.Join(store, "lua.org/v5.4.9/bin/lua.wasm"), wasmModule)

	code, stdout, errb := drive(t, "-out", out, "-store", store, "sqlite.org", "lua.org")
	if code != 0 {
		t.Fatalf("want 0, got %d: %s", code, errb)
	}
	m := readManifest(t, out)
	if m.Platform != "wasip1/wasm" {
		t.Errorf("platform %q", m.Platform)
	}
	if len(m.Modules) != 2 {
		t.Fatalf("want 2 modules, got %+v", m.Modules)
	}
	// Sorted by project, so a diff of the manifest is readable.
	if m.Modules[0].Project != "lua.org" || m.Modules[1].Project != "sqlite.org" {
		t.Errorf("not sorted: %+v", m.Modules)
	}
	lua := m.Modules[0]
	if lua.Version != "5.4.9" || lua.Bin != "lua" || lua.Path != "lua.org/lua.wasm" {
		t.Errorf("got %+v", lua)
	}
	if lua.Bytes != int64(len(wasmModule)) || len(lua.SHA256) != 64 {
		t.Errorf("size/sum wrong: %+v", lua)
	}
	// And the bytes are actually there, at the path the manifest promises.
	got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(lua.Path)))
	if err != nil || !bytes.Equal(got, wasmModule) {
		t.Errorf("the module was not staged where the manifest says: %v", err)
	}
	if !strings.Contains(stdout, "2 module(s) staged") {
		t.Errorf("the summary is missing:\n%s", stdout)
	}
}

// ⛔ A NAME ENDING IN .wasm IS A HYPOTHESIS. The factory checks the magic
// before publishing; so does this, because a text file copied into the site
// would be fetched by the page and fail inside the browser, where the error
// is somebody else's to read.
func TestAFileThatIsNotWebAssemblyIsNotStaged(t *testing.T) {
	out := t.TempDir()
	store := t.TempDir()
	withSeams(t, nil, nil)
	write(t, filepath.Join(store, "p.org/v1/bin/real.wasm"), wasmModule)
	write(t, filepath.Join(store, "p.org/v1/bin/README.wasm"), []byte("not a module\n"))
	write(t, filepath.Join(store, "p.org/v1/bin/truncated.wasm"), []byte{0x00, 'a'})
	write(t, filepath.Join(store, "p.org/v1/bin/helper"), wasmModule) // right bytes, wrong name

	code, _, errb := drive(t, "-out", out, "-store", store, "p.org")
	if code != 0 {
		t.Fatalf("want 0, got %d: %s", code, errb)
	}
	m := readManifest(t, out)
	if len(m.Modules) != 1 || m.Modules[0].Bin != "real" {
		t.Fatalf("want only the real module, got %+v", m.Modules)
	}
	for _, name := range []string{"README.wasm", "truncated.wasm", "helper"} {
		if _, err := os.Stat(filepath.Join(out, "p.org", name)); err == nil {
			t.Errorf("%s was staged", name)
		}
	}
}

// ⛔ AN EMPTY MANIFEST IS A FAILURE. A registry outage, a renamed platform and
// a typo all produce zero modules, and a page rendering "nothing available"
// is indistinguishable from one whose staging never ran.
func TestStagingNothingIsAnError(t *testing.T) {
	out := t.TempDir()
	withSeams(t, nil, nil)
	code, _, errb := drive(t, "-out", out, "-store", t.TempDir(), "p.org")
	if code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(errb, "print the same page") {
		t.Errorf("the reason was not given: %s", errb)
	}
	if _, err := os.Stat(filepath.Join(out, "index.json")); err == nil {
		t.Error("an empty manifest was written anyway")
	}
}

// One project with nothing is said and skipped; the run still succeeds on the
// strength of the others, because a sweep must not stop at the first gap.
func TestAProjectWithNoModuleIsSaidAndSkipped(t *testing.T) {
	out := t.TempDir()
	store := t.TempDir()
	withSeams(t, nil, nil)
	write(t, filepath.Join(store, "has.org/v1/bin/x.wasm"), wasmModule)

	code, _, errb := drive(t, "-out", out, "-store", store, "has.org", "empty.org")
	if code != 0 {
		t.Fatalf("want 0, got %d: %s", code, errb)
	}
	if !strings.Contains(errb, "empty.org staged no wasm module") {
		t.Errorf("the gap was not reported: %s", errb)
	}
	if len(readManifest(t, out).Modules) != 1 {
		t.Error("the other project was lost with it")
	}
}

func TestItRefusesBadInput(t *testing.T) {
	withSeams(t, nil, nil)
	for _, c := range []struct {
		what string
		args []string
	}{
		{"no -out", []string{"p.org"}},
		{"no projects", []string{"-out", t.TempDir()}},
		{"an unknown flag", []string{"-nope"}},
	} {
		if code, _, _ := drive(t, c.args...); code != 2 {
			t.Errorf("%s: want 2, got %d", c.what, code)
		}
	}
}

func TestAnInstallThatFailedIsReported(t *testing.T) {
	withSeams(t, errors.New("registry unreachable"), nil)
	code, _, errb := drive(t, "-out", t.TempDir(), "-store", t.TempDir(), "p.org")
	if code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(errb, "registry unreachable") {
		t.Errorf("the failure was not reported: %s", errb)
	}
	if !strings.Contains(errb, "not one project could be installed") {
		t.Errorf("a run that installed nothing must say so: %s", errb)
	}
}

// ⛔ ONE PROJECT'S ABSENCE COSTS ONLY THAT PROJECT, which is the whole reason
// the closure is not resolved here. Against the real registry, asking for
// sqlite.org's complete closure failed because gnu.org/readline is not
// published for wasip1/wasm — and a wasm32-wasip1 module is statically
// linked, so it never needed it. One shared resolution would have taken
// every other project down with it.
func TestOneProjectThatCannotBePickedDoesNotStopTheOthers(t *testing.T) {
	out := t.TempDir()
	store := t.TempDir()
	pp, pi, pt := stagePick, stageInstall, stageTempDir
	stagePick = func(project, _, _, _ string) (bottle.Ver, error) {
		if project == "absent.org" {
			return bottle.Ver{}, errors.New("no version is published for wasip1/wasm")
		}
		return bottle.ParseVer("1.0.0"), nil
	}
	stageInstall = func(bottle.Resolved, string, string, string) (bool, error) { return true, nil }
	t.Cleanup(func() { stagePick, stageInstall, stageTempDir = pp, pi, pt })
	write(t, filepath.Join(store, "has.org/v1.0.0/bin/x.wasm"), wasmModule)

	code, _, errb := drive(t, "-out", out, "-store", store, "absent.org", "has.org")
	if code != 0 {
		t.Fatalf("want 0 — the other project staged fine, got %d: %s", code, errb)
	}
	if !strings.Contains(errb, "absent.org") {
		t.Errorf("the missing project was not named: %s", errb)
	}
	if len(readManifest(t, out).Modules) != 1 {
		t.Error("the project that was there did not survive the one that was not")
	}
}

func TestATempDirItCannotMakeIsReported(t *testing.T) {
	withSeams(t, nil, nil)
	prev := stageTempDir
	stageTempDir = func(string, string) (string, error) { return "", errors.New("nowhere to write") }
	defer func() { stageTempDir = prev }()
	code, _, errb := drive(t, "-out", t.TempDir(), "p.org")
	if code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(errb, "nowhere to write") {
		t.Errorf("the failure was not reported: %s", errb)
	}
}

// A project with a path, which is most of them: the version is the element
// AFTER the whole project name, not after the first segment.
func TestTheVersionIsReadAfterTheWholeProjectName(t *testing.T) {
	for _, c := range []struct{ rel, project, want string }{
		{"lua.org/v5.4.9/bin/lua.wasm", "lua.org", "5.4.9"},
		{"github.com/simdjson/simdjson/v5.0.5/bin/m.wasm", "github.com/simdjson/simdjson", "5.0.5"},
		{"filippo.io/age/v1.3.2/bin/age.wasm", "filippo.io/age", "1.3.2"},
		{"p.org/notaversion/bin/x.wasm", "p.org", ""},
		{"p.org", "p.org", ""},
	} {
		if got := versionOf(c.rel, c.project); got != c.want {
			t.Errorf("versionOf(%q, %q) = %q, want %q", c.rel, c.project, got, c.want)
		}
	}
}
