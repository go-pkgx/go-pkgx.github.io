// Command wasmstage puts the registry's wasip1/wasm modules where a browser
// can reach them, and writes a manifest naming what it staged.
//
// # Why a staging step exists at all
//
// ⛔ ghcr.io DOES NOT DO CORS, so a page served from go-pkgx.github.io cannot
// pull a bottle from it. Measured 2026-10-29:
//
//	GET  /token?scope=…        200, and no access-control-allow-origin
//	OPTIONS /v2/…/manifests/…  405, and no CORS headers at all
//
// A playground that fetched bottles directly would fail in the browser and
// work in every test written outside one. So the fetching happens HERE, in
// CI, with a Go client that has no such restriction, and GitHub Pages then
// serves the result same-origin.
//
// ⛔ AND IT STAGES THE MODULE, NOT THE BOTTLE. The C lane publishes
// `.tar.zst`; a browser's DecompressionStream does gzip and deflate, and zstd
// is not universally available. Extracting `bin/<name>.wasm` here leaves the
// page with one `fetch` and an `instantiateStreaming`, no tar reader and no
// decompressor — which is also the only shape that stays correct when a lane
// changes its compression.
//
// # What it refuses to do
//
// ⛔ AN EMPTY MANIFEST IS A FAILURE, not a site with nothing to show. A
// registry outage, a renamed platform or a typo in a project name all produce
// zero modules, and a page that renders "nothing available" is
// indistinguishable from one whose staging never ran. This exits non-zero
// rather than publish an empty index.
//
// ⛔ A MODULE THAT IS NOT WEBASSEMBLY IS NOT STAGED. The file is checked for
// the `\0asm` magic before it is copied, for the same reason the factory
// checks it before publishing: a name ending in .wasm is a hypothesis, the
// first four bytes are the fact.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-pkgx/bottle"
)

// Seams, so the whole command is drivable without a network.
var (
	stagePick    = bottle.PickVersionFor
	stageInstall = bottle.InstallFor
	stageTempDir = os.MkdirTemp
)

// Module is one staged wasm module, as the page reads it.
type Module struct {
	Project string `json:"project"`
	Version string `json:"version"`
	Bin     string `json:"bin"`
	// Path is relative to the manifest, so the page can fetch it without
	// knowing where the site is mounted.
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Manifest is what the playground reads instead of asking a registry.
type Manifest struct {
	Platform  string   `json:"platform"`
	Generated string   `json:"generated"`
	Modules   []Module `json:"modules"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs_ := flag.NewFlagSet("wasmstage", flag.ContinueOnError)
	fs_.SetOutput(stderr)
	out := fs_.String("out", "", "directory to write the modules and index.json into (required)")
	store := fs_.String("store", "", "install into this directory (default: a temporary one, removed after)")
	generated := fs_.String("generated", "", "the timestamp to record (default: the store's own, for a reproducible run)")
	if err := fs_.Parse(args); err != nil {
		return 2
	}
	if *out == "" {
		fmt.Fprintln(stderr, "wasmstage: -out is required")
		return 2
	}
	projects := fs_.Args()
	if len(projects) == 0 {
		fmt.Fprintln(stderr, "wasmstage: name at least one project")
		return 2
	}

	dir := *store
	if dir == "" {
		d, err := stageTempDir("", "wasmstage-")
		if err != nil {
			fmt.Fprintln(stderr, "wasmstage:", err)
			return 1
		}
		dir = d
		defer os.RemoveAll(dir)
	}

	// ⛔ EACH PROJECT'S OWN BOTTLE, NOT ITS CLOSURE — and this was wrong the
	// first time. Asking for the complete closure failed against the real
	// registry on 2026-10-30:
	//
	//	no version of gnu.org/readline satisfies "8" AND is published for
	//	wasip1/wasm … asked for by 8 (sqlite.org)
	//
	// sqlite.org's wasm bottle is published and its declared dependency is
	// not, so the closure cannot complete — and it does not need to. A
	// wasm32-wasip1 module is STATICALLY LINKED: the `.wasm` carries
	// everything it runs, and the recipe's dependencies describe the native
	// build, not this one. Completing a closure here would have blocked every
	// project on the unavailability of something no browser is going to load.
	//
	// It also means one project failing costs only that project: there is no
	// shared resolution to abort.
	installed := 0
	for _, p := range projects {
		v, err := stagePick(p, "*", "wasip1", "wasm")
		if err != nil {
			fmt.Fprintf(stderr, "wasmstage: %s: %v\n", p, err)
			continue
		}
		if _, err := stageInstall(bottle.Resolved{Project: p, Version: v}, dir, "wasip1", "wasm"); err != nil {
			fmt.Fprintf(stderr, "wasmstage: %s: %v\n", p, err)
			continue
		}
		installed++
	}
	if installed == 0 {
		fmt.Fprintln(stderr, "wasmstage: not one project could be installed — this is a failed run, not an empty platform")
		return 1
	}

	mods, err := stage(dir, *out, projects, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "wasmstage:", err)
		return 1
	}
	if len(mods) == 0 {
		fmt.Fprintln(stderr, "wasmstage: no wasm module was staged — a registry that answered nothing and a site with nothing to show print the same page, so this is a failure")
		return 1
	}

	m := Manifest{Platform: "wasip1/wasm", Generated: *generated, Modules: mods}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "wasmstage:", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(*out, "index.json"), append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(stderr, "wasmstage:", err)
		return 1
	}
	for _, mod := range mods {
		fmt.Fprintf(stdout, "%-36s %-10s %-18s %8d bytes\n", mod.Project, mod.Version, mod.Bin, mod.Bytes)
	}
	fmt.Fprintf(stdout, "\n%d module(s) staged into %s\n", len(mods), *out)
	return 0
}

// stage copies every wasm module the named projects ship into out.
func stage(dir, out string, projects []string, stderr io.Writer) ([]Module, error) {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	var mods []Module
	for _, p := range projects {
		root := filepath.Join(dir, filepath.FromSlash(p))
		found := 0
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".wasm") {
				return nil
			}
			ok, err := isWasm(path)
			if err != nil || !ok {
				return nil
			}
			rel, rerr := filepath.Rel(dir, path)
			if rerr != nil {
				return rerr
			}
			version := versionOf(rel, p)
			dst := filepath.Join(out, filepath.FromSlash(p), d.Name())
			sum, n, cerr := copyFile(path, dst)
			if cerr != nil {
				return cerr
			}
			mods = append(mods, Module{
				Project: p,
				Version: version,
				Bin:     strings.TrimSuffix(d.Name(), ".wasm"),
				Path:    filepath.ToSlash(filepath.Join(p, d.Name())),
				Bytes:   n,
				SHA256:  sum,
			})
			found++
			return nil
		})
		if err != nil {
			return nil, err
		}
		// Said, not fatal: a sweep over a project list must not stop because
		// one project has no module, and the empty-manifest check above is
		// what catches "none of them did".
		if found == 0 {
			fmt.Fprintf(stderr, "wasmstage: %s staged no wasm module\n", p)
		}
	}
	sort.Slice(mods, func(i, j int) bool {
		if mods[i].Project != mods[j].Project {
			return mods[i].Project < mods[j].Project
		}
		return mods[i].Bin < mods[j].Bin
	})
	return mods, nil
}

// versionOf reads the `v<version>` element that follows the project in a
// store-relative path.
func versionOf(rel, project string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	n := len(strings.Split(project, "/"))
	if len(parts) > n && strings.HasPrefix(parts[n], "v") {
		return strings.TrimPrefix(parts[n], "v")
	}
	return ""
}

// isWasm reports whether a file begins with the WebAssembly magic number.
//
// ⛔ THE NAME IS A HYPOTHESIS, THE FIRST FOUR BYTES ARE THE FACT — the same
// rule go-pkgx/bk arrived at the hard way, having counted a GDB
// pretty-printer called `libstdc++.so.6.0.33-gdb.py` as a shared library and
// then refused a real `libfolly.so.0.58.0-dev` for its version suffix.
func isWasm(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var b [4]byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		return false, nil // too short to be a module, and not an error here
	}
	return b == [4]byte{0x00, 'a', 's', 'm'}, nil
}

// copyFile writes src to dst and returns its sha256 and size.
func copyFile(src, dst string) (string, int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	outF, err := os.Create(dst)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(outF, h), in)
	if cerr := outF.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
