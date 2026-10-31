// Command vulnmods asks a vulnerability database about EVERY Go module in this
// repository, and refuses to call a module clean that it could not read.
//
//	vulnmods [-root DIR] [-govulncheck BIN] [-vulngate BIN] [-go VERSION]
//
// # ⛔ WHY THIS EXISTS: A NAMED DIRECTORY STOPPED BEING THE WHOLE ANSWER
//
// vulngate.yml scanned `tools/versioncheck`, and its header explains at length
// why that directory and not the root — the root module is the SITE's, pinning
// the landingkit Hugo module with no Go packages in it, so a scan there writes
// a report with no SBOM and vulngate rightly refuses it.
//
// That reasoning was right and it named ONE directory. By 2026-10-31 the
// repository had four modules: the root, tools/versioncheck, tools/wasmstage
// (added with the playground staging tool) and tools/playground (added with
// the WASI confrontation). Two of them had never been asked about anything,
// and tools/wasmstage is the one with a real dependency surface — an OCI
// client, x/crypto, two decompressors — and the one whose output is deployed.
//
// Nothing was wrong with the workflow. The repository grew around it. That is
// the same failure the .gitignore in this repository is an ALLOW-LIST for, and
// the same one tools/versioncheck itself was written for: a list of names is
// correct on the day it is written.
//
// So this DISCOVERS the modules instead of being told them. Adding a fifth
// tool brings it into the scan with no edit here and no edit to the workflow.
//
// # ⛔ A MODULE WITH NO PACKAGES IS SKIPPED, AND SAYING SO IS THE POINT
//
// The root module genuinely has no Go packages, so govulncheck cannot scan it
// and must not be reported as clean. It is counted as SKIPPED with its reason
// printed. But "every module was skipped" is indistinguishable from "nothing
// was scanned", so a run in which NO module was actually scanned FAILS — that
// is the positive control, and without it a repository that stopped building
// would report a clean bill forever.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// state is what one module's scan concluded.
type state string

const (
	clean   state = "CLEAN"
	called  state = "CALLED"
	skipped state = "SKIPPED"
	unread  state = "UNREAD"
)

type result struct {
	dir    string
	state  state
	detail string
}

func main() {
	root := flag.String("root", ".", "repository root to discover modules under")
	govulncheck := flag.String("govulncheck", "govulncheck", "govulncheck binary")
	vulngate := flag.String("vulngate", "vulngate", "vulngate binary (the judge)")
	goVersion := flag.String("go", "", "toolchain version passed to vulngate -go (default: `go env GOVERSION`)")
	flag.Parse()

	mods, err := modules(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vulnmods:", err)
		os.Exit(1)
	}
	if len(mods) == 0 {
		// Discovery finding nothing would make every later count agree with
		// every possible repository.
		fmt.Fprintf(os.Stderr, "vulnmods: no go.mod found under %s: nothing was scanned\n", *root)
		os.Exit(1)
	}

	if *goVersion == "" {
		out, err := exec.Command("go", "env", "GOVERSION").Output()
		if err != nil {
			fmt.Fprintln(os.Stderr, "vulnmods: go env GOVERSION:", err)
			os.Exit(1)
		}
		*goVersion = strings.TrimSpace(string(out))
	}

	fmt.Printf("%d module(s) discovered under %s, judged against %s\n\n", len(mods), *root, *goVersion)

	var results []result
	for _, dir := range mods {
		// `dir` is relative to -root and is what the report shows; the scan
		// has to run in the real directory. Passing the display name as the
		// working directory made every module unreadable and scanned the tool
		// itself as if it were the root.
		path := filepath.Join(*root, filepath.FromSlash(dir))
		pkgs, err := buildablePackages(path)
		if err != nil {
			results = append(results, result{dir, unread, err.Error()})
			continue
		}
		if pkgs == 0 {
			results = append(results, result{dir, skipped, "go list ./... matches no packages: nothing to scan"})
			continue
		}
		results = append(results, scan(path, dir, *govulncheck, *vulngate, *goVersion))
	}

	var scanned, bad, unreadable int
	for _, r := range results {
		fmt.Printf("%-8s %-24s %s\n", r.state, r.dir, r.detail)
		switch r.state {
		case clean:
			scanned++
		case called:
			scanned++
			bad++
		case unread:
			unreadable++
		}
	}

	fmt.Printf("\n%d module(s): %d scanned, %d reachable, %d unreadable, %d skipped\n",
		len(results), scanned, bad, unreadable, len(results)-scanned-unreadable)

	switch {
	case scanned == 0:
		fmt.Fprintln(os.Stderr, "\n⛔ NO module was actually scanned. Every one was skipped or unreadable,\n"+
			"   which is not the same as finding nothing.")
		os.Exit(1)
	case unreadable > 0:
		fmt.Fprintln(os.Stderr, "\n⛔ A module could not be read. This run says nothing about it.")
		os.Exit(1)
	case bad > 0:
		fmt.Fprintln(os.Stderr, "\n⛔ A vulnerability is REACHABLE from this repository's code.")
		os.Exit(1)
	}
}

// buildablePackages asks the Go tool how many packages a module actually has.
//
// This is the measurable form of the root module's exception, and the version
// that counts .go files was WRONG: this repository's root has exactly one,
// `kit.go`, behind `//go:build tools`, which exists only so `go mod tidy` does
// not delete the landingkit requirement. It never builds. `go list ./...`
// there says "matched no packages", which is the same question govulncheck's
// own loader asks.
//
// Measuring it this way rather than reading govulncheck's REFUSAL matters:
// vulngate refuses a report with no SBOM, and a module that has stopped
// building produces that same refusal. Deciding from the refusal alone that
// everything is fine is the one inference that must never be automatic.
func buildablePackages(dir string) (int, error) {
	cmd := exec.Command("go", "list", "-e", "-f", "{{.ImportPath}}", "./...")
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("go list in %s: %w: %s", dir, err, firstLine(strings.TrimSpace(errb.String())))
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}

// modules returns every directory under root holding a go.mod, sorted, with
// the usual non-source trees pruned.
func modules(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "public", "resources", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			dir := filepath.Dir(path)
			rel, err := filepath.Rel(root, dir)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// scan runs govulncheck in one module and hands its report to vulngate.
//
// govulncheck's own exit status is deliberately ignored: it exits non-zero over
// findings it has decided are NOT called, and zero over some it has, which is
// the whole reason vulngate exists. The report being EMPTY is checked
// separately, because a scan that wrote nothing must not read as nothing found.
func scan(dir, name, govulncheck, vulngate, goVersion string) result {
	cmd := exec.Command(govulncheck, "-format", "json", "./...")
	cmd.Dir = dir
	dir = name // every result below is reported under the name, not the path
	var report, errb bytes.Buffer
	cmd.Stdout = &report
	cmd.Stderr = &errb

	_ = cmd.Run() // see above: the status is not the verdict
	stderr := strings.TrimSpace(errb.String())

	if report.Len() == 0 {
		// The root module's shape: a go.mod with no Go packages in it. That is
		// a real and permanent property, not a failure, but it is reported as
		// a skip with its reason rather than counted as a pass.
		if strings.Contains(stderr, "no packages matched") || strings.Contains(stderr, "no Go files") {
			return result{dir, skipped, "no Go packages in this module: " + firstLine(stderr)}
		}
		return result{dir, unread, "govulncheck wrote nothing: " + firstLine(stderr)}
	}

	tmp, err := os.CreateTemp("", "vulnmods-*.json")
	if err != nil {
		return result{dir, unread, err.Error()}
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(report.Bytes()); err != nil {
		tmp.Close()
		return result{dir, unread, err.Error()}
	}
	tmp.Close()

	judge := exec.Command(vulngate, "-go", goVersion, tmp.Name())
	var jout bytes.Buffer
	judge.Stdout = &jout
	judge.Stderr = &jout
	err = judge.Run()
	detail := firstLine(strings.TrimSpace(jout.String()))
	if err != nil {
		// vulngate refuses a report with no SBOM in it, and that refusal is
		// UNREAD rather than a finding: it means the scan did not run.
		if strings.Contains(jout.String(), "no SBOM") {
			return result{dir, unread, detail}
		}
		return result{dir, called, detail}
	}
	return result{dir, clean, detail}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
