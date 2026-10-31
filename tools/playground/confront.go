// Package playground confronts the browser playground's WASI shim with
// wasmtime, the reference implementation, on the same modules and the same
// arguments.
//
// # Why this package is a test and not a page
//
// A playground that renders is not a playground that WORKS. A WASI host can
// return the wrong errno, write a struct field at the wrong offset, or silently
// answer "success" to a call it never implemented, and the page will still look
// exactly right — the module will just produce output nobody can check. The
// only thing that distinguishes a working shim from a convincing one is a
// second implementation answering the same questions.
//
// So: run each module under wasmtime, record stdout, stderr and the exit status
// as a FIXTURE, then run the same module through the page's own
// `static/playground/wasi.js` and require the three to match byte for byte.
//
// # What it needs, and what it does when it does not have it
//
// The modules are not in this repository — no .wasm is committed — so these
// tests SKIP, loudly, unless told where a staged tree is:
//
//	PLAYGROUND_WASM_DIR   a directory written by tools/wasmstage
//	WASMTIME              the wasmtime binary (default: "wasmtime" on PATH)
//	NODE                  a JavaScript runtime (default: "node" on PATH)
//
// A skip is not a pass. The fixtures carry each module's SHA-256, so a fixture
// that no longer matches tells you WHICH of the two changed: the module, or the
// behaviour.
package playground

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Case is one module run: what to run it with, and what the reference produced.
type Case struct {
	Name    string `json:"name"`
	Project string `json:"project"`
	Version string `json:"version"`
	Bin     string `json:"bin"`
	// Module is relative to the staged tree, exactly as wasmstage's index.json
	// spells it.
	Module string `json:"module"`
	SHA256 string `json:"sha256"`
	// Args are the guest's arguments AFTER argv[0], which is always Bin. CI
	// passes none and lets wasmtime use the module path; pinning argv[0] to the
	// binary's name is what keeps a fixture from embedding this machine's
	// temporary directory.
	Args []string `json:"args"`
	// Files is the working directory the run sees, name -> base64 contents.
	Files map[string]string `json:"files,omitempty"`
	// Stdin is base64, and may be empty.
	Stdin string `json:"stdin,omitempty"`

	// What wasmtime produced. Base64 because minigzip's stdout is gzip.
	WantExit   int    `json:"want_exit"`
	WantStdout string `json:"want_stdout"`
	WantStderr string `json:"want_stderr"`
}

// Fixtures is the whole recorded confrontation.
type Fixtures struct {
	// Comment is for the person who opens the file, not for the code.
	Comment string `json:"_comment"`
	// Withdrawn records a module that WAS confronted and no longer is, with the
	// reason and the condition for putting it back.
	//
	// It is a field rather than a JSON comment — JSON has none — and it is in
	// this struct rather than only in the file because `Save` round-trips the
	// struct: a `-update` run would otherwise silently delete the explanation
	// for why the set is smaller than it looks, which is the only thing
	// stopping someone restoring a module into a gate that cannot be green.
	Withdrawn string `json:"_withdrawn,omitempty"`
	Recorded  string `json:"recorded"`
	Wasmtime  string `json:"wasmtime"`
	// GuestCwd is the directory the guest believes it is in. wasmtime is told
	// the host path through $PWD; the shim's in-memory filesystem uses this
	// fixed one, so a fixture does not depend on where it was recorded.
	GuestCwd string `json:"guest_cwd"`
	Cases    []Case `json:"cases"`
}

// Result is what one run produced, whichever runtime produced it.
type Result struct {
	Exit        int
	Stdout      []byte
	Stderr      []byte
	Diagnostics []Diagnostic
	Trap        string
}

// Diagnostic is one syscall the shim could not answer, or had to approximate.
type Diagnostic struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Count  int    `json:"count"`
}

// LoadFixtures reads the recorded confrontation.
func LoadFixtures(path string) (*Fixtures, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f Fixtures
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(f.Cases) == 0 {
		// An empty fixture file and a fixture file that was never written
		// produce the same green test run, which is the failure mode this
		// whole package exists to avoid.
		return nil, fmt.Errorf("%s: no cases — an empty oracle proves nothing", path)
	}
	return &f, nil
}

// Save writes the fixtures back, formatted so a diff is readable.
func (f *Fixtures) Save(path string) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// materialise writes a case's working directory into dir and returns the
// stdin bytes.
func (c *Case) materialise(dir string) ([]byte, error) {
	for name, b64 := range c.Files {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("%s: file %q: %w", c.Name, name, err)
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, err
		}
	}
	if c.Stdin == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(c.Stdin)
}

// RunWasmtime runs one case under the reference implementation.
//
// The flags are the ones go-pkgx/packages' wasip1.yml and wasip1-c.yml use to
// prove a bottle before publishing it: the filesystem is granted explicitly
// (WASI has no ambient one) and $PWD tells Go's wasip1 runtime which preopen
// the working directory is in. `--argv0` is added here, and only here, so the
// recorded output cannot contain the path this ran from.
func RunWasmtime(ctx context.Context, wasmtimeBin, stagedDir string, c *Case) (*Result, error) {
	work, err := os.MkdirTemp("", "playground-wasmtime-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	stdin, err := c.materialise(work)
	if err != nil {
		return nil, err
	}

	module := filepath.Join(stagedDir, filepath.FromSlash(c.Module))
	args := []string{"run", "--dir=/", "--env", "PWD=" + work, "--argv0", c.Bin, module}
	args = append(args, c.Args...)

	cmd := exec.CommandContext(ctx, wasmtimeBin, args...)
	cmd.Dir = work
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		code = ee.ExitCode()
	}
	return &Result{Exit: code, Stdout: out.Bytes(), Stderr: errb.Bytes()}, nil
}

// shimSpec is the JSON testdata/runshim.mjs reads.
type shimSpec struct {
	Module string            `json:"module"`
	Argv   []string          `json:"argv"`
	Env    map[string]string `json:"env"`
	Stdin  string            `json:"stdin,omitempty"`
	Files  map[string]string `json:"files,omitempty"`
}

type shimOut struct {
	Exit        int          `json:"exit"`
	Stdout      string       `json:"stdout"`
	Stderr      string       `json:"stderr"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Trap        string       `json:"trap,omitempty"`
	Error       string       `json:"error,omitempty"`
}

// ShimHarness is a directory holding the page's wasi.js (as wasi.mjs, because
// Node decides a file's module kind by its extension) next to runshim.mjs.
type ShimHarness struct {
	Dir  string
	Node string
}

// pageScripts are the browser files, in the order they are staged. Node picks
// a file's module kind from its extension and the page needs the .js names, so
// each is copied as .mjs and its import specifiers rewritten to match — the
// ONLY edit made to them. Everything else a test observes is the published
// file, byte for byte.
var pageScripts = []string{"wasi.js", "presets.js", "app.js"}

// NewShimHarness copies the PUBLISHED page scripts — not copies of them kept in
// step by hand — into dir, alongside the Node runners.
func NewShimHarness(nodeBin, repoRoot, dir string) (*ShimHarness, error) {
	for _, name := range pageScripts {
		b, err := os.ReadFile(filepath.Join(repoRoot, "static", "playground", name))
		if err != nil {
			return nil, err
		}
		src := string(b)
		for _, other := range pageScripts {
			src = strings.ReplaceAll(src, `"./`+other+`"`, `"./`+strings.TrimSuffix(other, ".js")+`.mjs"`)
		}
		out := filepath.Join(dir, strings.TrimSuffix(name, ".js")+".mjs")
		if err := os.WriteFile(out, []byte(src), 0o644); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"runshim.mjs", "loadpage.mjs"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, "tools", "playground", "testdata", name))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return nil, err
		}
	}
	return &ShimHarness{Dir: dir, Node: nodeBin}, nil
}

// LoadPage imports every script the page loads — which parses and evaluates
// them — and returns the presets app.js puts in front of the reader.
func (h *ShimHarness) LoadPage(ctx context.Context) (map[string]Preset, error) {
	cmd := exec.CommandContext(ctx, h.Node, filepath.Join(h.Dir, "loadpage.mjs"))
	cmd.Dir = h.Dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("the page's scripts did not load: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	var presets map[string]Preset
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &presets); err != nil {
		return nil, fmt.Errorf("loadpage printed %q: %w", out.String(), err)
	}
	return presets, nil
}

// Preset is one module's default command line, as the page offers it.
type Preset struct {
	Args  []string          `json:"args"`
	Stdin string            `json:"stdin"`
	Files map[string]string `json:"files"`
	// Roundtrip is Args after the page has rendered them into its command-line
	// box and parsed them back out — the argv a reader pressing Run actually
	// gets. Comparing Args alone would miss a quoting bug entirely.
	Roundtrip []string `json:"roundtrip"`
}

// Run puts one case through the page's shim.
func (h *ShimHarness) Run(ctx context.Context, stagedDir, guestCwd string, c *Case) (*Result, error) {
	files := map[string]string{}
	for name, b64 := range c.Files {
		files[guestCwd+"/"+name] = b64
	}
	spec := shimSpec{
		Module: filepath.Join(stagedDir, filepath.FromSlash(c.Module)),
		Argv:   append([]string{c.Bin}, c.Args...),
		Env:    map[string]string{"PWD": guestCwd},
		Stdin:  c.Stdin,
		Files:  files,
	}
	b, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	specPath := filepath.Join(h.Dir, "spec-"+c.Name+".json")
	if err := os.WriteFile(specPath, b, 0o644); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, h.Node, filepath.Join(h.Dir, "runshim.mjs"), specPath)
	cmd.Dir = h.Dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: node: %w: %s", c.Name, err, strings.TrimSpace(errb.String()))
	}
	var so shimOut
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &so); err != nil {
		return nil, fmt.Errorf("%s: harness printed %q: %w", c.Name, out.String(), err)
	}
	if so.Error != "" {
		return nil, fmt.Errorf("%s: harness: %s", c.Name, so.Error)
	}
	stdout, err := base64.StdEncoding.DecodeString(so.Stdout)
	if err != nil {
		return nil, err
	}
	stderr, err := base64.StdEncoding.DecodeString(so.Stderr)
	if err != nil {
		return nil, err
	}
	return &Result{Exit: so.Exit, Stdout: stdout, Stderr: stderr, Diagnostics: so.Diagnostics, Trap: so.Trap}, nil
}

// Stamp is the timestamp written into a regenerated fixture file.
func Stamp() string { return time.Now().UTC().Format(time.RFC3339) }
