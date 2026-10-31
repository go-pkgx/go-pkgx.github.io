package playground

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "re-record the fixtures from wasmtime")

const fixturesPath = "testdata/fixtures.json"

const repoRoot = "../.."

func stagedDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("PLAYGROUND_WASM_DIR")
	if dir == "" {
		// The names come from the fixtures, not from a copy in this string. A
		// fourth hand-typed list would be one more thing to forget, and this
		// one is read by whoever is trying to run the tests at all.
		t.Skipf("PLAYGROUND_WASM_DIR is not set: stage the modules first "+
			"(cd tools/wasmstage && go run . -out <dir> %s). "+
			"A SKIP here is not a pass — nothing was confronted.",
			strings.Join(fixtureProjects(t), " "))
	}
	return dir
}

func tool(t *testing.T, env, fallback string) string {
	t.Helper()
	if v := os.Getenv(env); v != "" {
		return v
	}
	p, err := exec.LookPath(fallback)
	if err != nil {
		t.Skipf("%s not found and $%s unset: nothing was confronted", fallback, env)
	}
	return p
}

// TestFixturesWellFormed needs no runtime at all, so a repository with no
// wasmtime still notices a fixture file that was truncated, hand-edited into
// invalid base64, or emptied.
func TestFixturesWellFormed(t *testing.T) {
	f, err := LoadFixtures(fixturesPath)
	if err != nil {
		t.Fatal(err)
	}
	if f.GuestCwd == "" {
		t.Fatal("guest_cwd is empty: the shim would resolve relative paths against nothing")
	}
	seen := map[string]bool{}
	for _, c := range f.Cases {
		if c.Name == "" || c.Bin == "" || c.Module == "" {
			t.Fatalf("case %+v: name, bin and module are all required", c)
		}
		if seen[c.Name] {
			t.Fatalf("two cases named %q", c.Name)
		}
		seen[c.Name] = true
		if len(c.SHA256) != 64 {
			t.Fatalf("%s: sha256 %q is not 64 hex characters", c.Name, c.SHA256)
		}
		for _, b64 := range []string{c.WantStdout, c.WantStderr, c.Stdin} {
			if b64 == "" {
				continue
			}
			if _, err := base64.StdEncoding.DecodeString(b64); err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
		}
	}
	// ⛔ SEVEN, AND THE MISSING ONE MUST BE ACCOUNTED FOR.
	//
	// This said eight until 2026-10-31, when simdjson-minify was withdrawn
	// because its registry tag serves two different modules at different times
	// (go-pkgx/packages#393). Lowering a floor is exactly how a population
	// quietly shrinks, so the floor is paired with a requirement that the file
	// SAY why: a future reduction to six has to explain itself in the same
	// place, and a restoration has to remove the note.
	if len(f.Cases) < 7 {
		t.Fatalf("%d cases: fewer than the modules this page confronts", len(f.Cases))
	}
	if len(f.Cases) < 8 && f.Withdrawn == "" {
		t.Fatal("fewer than eight cases and no `_withdrawn` note saying which module " +
			"left and why: a set that shrank without a reason is indistinguishable " +
			"from one that was never complete")
	}
}

// TestWasmtimeMatchesFixtures is the regression half: the reference is re-run
// and must still say what the fixture recorded.
func TestWasmtimeMatchesFixtures(t *testing.T) {
	dir := stagedDir(t)
	wasmtime := tool(t, "WASMTIME", "wasmtime")

	f, err := LoadFixtures(fixturesPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if *update {
		ver, err := exec.CommandContext(ctx, wasmtime, "--version").Output()
		if err != nil {
			t.Fatal(err)
		}
		f.Wasmtime = strings.TrimSpace(string(ver))
		f.Recorded = Stamp()
	}

	for i := range f.Cases {
		c := &f.Cases[i]
		t.Run(c.Name, func(t *testing.T) {
			sum := moduleSum(t, dir, c.Module)
			if *update {
				c.SHA256 = sum
			} else if sum != c.SHA256 {
				t.Fatalf("%s: the staged module is not the one the fixture was recorded from\n"+
					" staged  %s\n fixture %s\nRe-record with -update, and say so.", c.Module, sum, c.SHA256)
			}
			got, err := RunWasmtime(ctx, wasmtime, dir, c)
			if err != nil {
				t.Fatal(err)
			}
			if *update {
				c.WantExit = got.Exit
				c.WantStdout = base64.StdEncoding.EncodeToString(got.Stdout)
				c.WantStderr = base64.StdEncoding.EncodeToString(got.Stderr)
				t.Logf("recorded: exit=%d stdout=%d bytes stderr=%d bytes", got.Exit, len(got.Stdout), len(got.Stderr))
				return
			}
			compare(t, "wasmtime", c, got)
		})
	}

	if *update {
		if err := f.Save(fixturesPath); err != nil {
			t.Fatal(err)
		}
		t.Logf("re-recorded %s", fixturesPath)
	}
}

// TestShimMatchesWasmtime is the confrontation: the page's own wasi.js, over a
// plain ArrayBuffer, answering the same modules with the same arguments.
//
// ⛔ It compares against the fixture wasmtime produced, not against "it did not
// throw". A shim that runs a module to completion and prints the wrong bytes is
// the failure this exists to catch.
func TestShimMatchesWasmtime(t *testing.T) {
	dir := stagedDir(t)
	node := tool(t, "NODE", "node")

	f, err := LoadFixtures(fixturesPath)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewShimHarness(node, repoRoot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for i := range f.Cases {
		c := &f.Cases[i]
		t.Run(c.Name, func(t *testing.T) {
			if sum := moduleSum(t, dir, c.Module); sum != c.SHA256 {
				t.Fatalf("%s: staged module %s is not the fixture's %s", c.Module, sum, c.SHA256)
			}
			got, err := h.Run(ctx, dir, f.GuestCwd, c)
			if err != nil {
				t.Fatal(err)
			}
			if got.Trap != "" {
				t.Fatalf("the shim trapped where wasmtime exited %d:\n%s", c.WantExit, got.Trap)
			}
			for _, d := range got.Diagnostics {
				t.Logf("diagnostic: %s x%d — %s", d.Name, d.Count, d.Detail)
			}
			compare(t, "the shim", c, got)
		})
	}
}

// TestShimReportsAnUnimplementedSyscall is the control.
//
// Every other test here would pass just as happily if `_unimplemented` were
// never reached — so this plants one. The probe calls os.Symlink, which lowers
// to path_symlink, which this host does NOT implement. The test demands that
// the guest sees an error AND that the shim recorded a diagnostic naming the
// call. Without it, "no diagnostics" would be indistinguishable from "the
// diagnostic channel does not work".
func TestShimReportsAnUnimplementedSyscall(t *testing.T) {
	node := tool(t, "NODE", "node")
	goBin := tool(t, "GO", "go")

	work := t.TempDir()
	module := filepath.Join(work, "probe.wasm")
	build := exec.Command(goBin, "build", "-trimpath", "-o", module, ".")
	build.Dir = filepath.Join("testdata", "probe")
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build the wasip1 probe (%v): %s", err, out)
	}

	h, err := NewShimHarness(node, repoRoot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &Case{Name: "probe", Bin: "probe", Module: "probe.wasm"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	got, err := h.Run(ctx, work, "/work", c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Trap != "" {
		t.Fatalf("the probe trapped: %s", got.Trap)
	}
	out := string(got.Stdout)
	t.Logf("probe stdout:\n%s", out)
	if !strings.Contains(out, "symlink: error") {
		t.Fatalf("the probe did not see path_symlink fail; the ENOSYS path is not reachable:\n%s", out)
	}
	found := false
	for _, d := range got.Diagnostics {
		t.Logf("diagnostic: %s x%d — %s", d.Name, d.Count, d.Detail)
		if d.Name == "path_symlink" {
			found = true
		}
	}
	if !found {
		t.Fatal("no diagnostic named path_symlink: an unimplemented syscall was invisible, which is the one outcome this shim must not have")
	}
	// And the other half of the control: a call that IS implemented must not
	// be reported as missing, or the channel cries wolf.
	for _, d := range got.Diagnostics {
		if d.Name == "fd_write" || d.Name == "path_open" {
			t.Fatalf("%s was reported as unimplemented, but this host implements it", d.Name)
		}
	}
}

// TestPagePresetsAreTheConfrontedInvocations ties the two halves together.
//
// The fixtures prove that a command line behaves the same here as under
// wasmtime. That proof is worth nothing to a reader if the page then offers a
// DIFFERENT command line — so the defaults the page shows must be, argument for
// argument and byte for byte, the ones that were confronted.
//
// Loading the page's scripts is also the only check available here that they
// parse at all: there is no browser in this environment.
func TestPagePresetsAreTheConfrontedInvocations(t *testing.T) {
	node := tool(t, "NODE", "node")
	f, err := LoadFixtures(fixturesPath)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewShimHarness(node, repoRoot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	presets, err := h.LoadPage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range f.Cases {
		p, ok := presets[c.Bin]
		if !ok {
			t.Errorf("%s: the page has no preset, so it would offer a bare command line for a module whose only proven invocation is %v", c.Bin, c.Args)
			continue
		}
		if strings.Join(p.Args, "\x00") != strings.Join(c.Args, "\x00") {
			t.Errorf("%s: the page offers %q, the fixture confronted %q", c.Bin, p.Args, c.Args)
		}
		// And what survives the command-line box, which is the argv the
		// module is actually handed. argv[0] is the binary's name, as it is
		// in the fixture runs.
		wantArgv := append([]string{c.Bin}, c.Args...)
		if strings.Join(p.Roundtrip, "\x00") != strings.Join(wantArgv, "\x00") {
			t.Errorf("%s: quoting then re-parsing the preset gives %q, not %q — the page would run something else",
				c.Bin, p.Roundtrip, wantArgv)
		}
		wantStdin, _ := base64.StdEncoding.DecodeString(c.Stdin)
		if p.Stdin != string(wantStdin) {
			t.Errorf("%s: the page's stdin is %q, the fixture's %q", c.Bin, p.Stdin, string(wantStdin))
		}
		if len(p.Files) != len(c.Files) {
			t.Errorf("%s: the page seeds %d file(s), the fixture %d", c.Bin, len(p.Files), len(c.Files))
			continue
		}
		for name, b64 := range c.Files {
			want, _ := base64.StdEncoding.DecodeString(b64)
			if p.Files[name] != string(want) {
				t.Errorf("%s: file %q is %q on the page and %q in the fixture", c.Bin, name, p.Files[name], string(want))
			}
		}
	}
	for bin := range presets {
		found := false
		for _, c := range f.Cases {
			if c.Bin == bin {
				found = true
			}
		}
		if !found {
			t.Errorf("the page offers a preset for %q, which no fixture confronts", bin)
		}
	}
}

func compare(t *testing.T, who string, c *Case, got *Result) {
	t.Helper()
	wantOut, _ := base64.StdEncoding.DecodeString(c.WantStdout)
	wantErr, _ := base64.StdEncoding.DecodeString(c.WantStderr)
	if got.Exit != c.WantExit {
		t.Errorf("%s: exit %d, fixture %d", who, got.Exit, c.WantExit)
	}
	if !bytes.Equal(got.Stdout, wantOut) {
		t.Errorf("%s: stdout differs\n got (%d bytes) %s\nwant (%d bytes) %s",
			who, len(got.Stdout), preview(got.Stdout), len(wantOut), preview(wantOut))
	}
	if !bytes.Equal(got.Stderr, wantErr) {
		t.Errorf("%s: stderr differs\n got (%d bytes) %s\nwant (%d bytes) %s",
			who, len(got.Stderr), preview(got.Stderr), len(wantErr), preview(wantErr))
	}
}

// preview shows bytes a person can read: minigzip's stdout is gzip, and
// printing it raw would fill the failure with terminal noise.
func preview(b []byte) string {
	const max = 200
	printable := true
	for _, c := range b {
		if c < 0x09 || (c > 0x0d && c < 0x20) || c == 0x7f {
			printable = false
			break
		}
	}
	if !printable {
		if len(b) > 32 {
			return hex.EncodeToString(b[:32]) + "… (binary)"
		}
		return hex.EncodeToString(b) + " (binary)"
	}
	s := string(b)
	if len(s) > max {
		return strconv.Quote(s[:max]) + "…"
	}
	return strconv.Quote(s)
}

func moduleSum(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%v — is PLAYGROUND_WASM_DIR a tree written by tools/wasmstage?", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
