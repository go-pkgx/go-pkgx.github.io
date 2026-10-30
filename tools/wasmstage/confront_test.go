// Confrontation: the DERIVED list against the factory that builds the bottles.
//
// # Why parsing the catalogue is not enough
//
// ⛔ A DERIVATION THAT RETURNS A SUBSET LOOKS EXACTLY LIKE A SMALL CATALOGUE.
// Every test in main_test.go drives `derive` against a catalogue this file
// wrote, so every one of them proves only that the filter does what the
// filter was written to do. If the real catalogue carried six of the seven
// projects — a publish that raced the catalogue job, a platform string that
// moved, a `Versions` the factory left empty — `-derive` would return six,
// the manifest would be valid, the page would render, and nothing would be
// red. There is no internal evidence that distinguishes that from a registry
// that genuinely holds six.
//
// So the answer is confronted with the ground truth, which lives in another
// repository: go-pkgx/packages/.github/workflows/wasip1.yml (the Go lane) and
// wasip1-c.yml (the C/C++ lane). Their `matrix.include` entries ARE the set of
// projects built and published for wasip1/wasm — read 2026-10-30:
//
//	wasip1.yml    filippo.io/age, github.com/cpuguy83/go-md2man, mvdan.cc/sh
//	wasip1-c.yml  zlib.net, lua.org, sqlite.org, github.com/simdjson/simdjson
//
// # IT CAUGHT ONE ON ITS FIRST REAL RUN
//
// 2026-10-30, against the catalogue generated 20:37:18Z that day — the first
// one ever published for wasip1/wasm:
//
//	the factory declares 7 … github.com/simdjson/simdjson …
//	the catalogue yields 6
//	BUILT BUT NOT DERIVED: github.com/simdjson/simdjson
//
// The discriminator below settles it: wasip1-c.yml has listed simdjson since
// 2026-10-26 19:35 (go-pkgx/packages#377), four days BEFORE that catalogue was
// generated, so this is not a lane that moved after the snapshot. And the
// bottle is there — a pinned `wasmstage … github.com/simdjson/simdjson`
// installs v5.0.5 and stages 432 531 bytes that pass the \0asm check. So the
// bottle is published, the catalogue does not list it, and the gap is in the
// catalogue builder.
//
// Which is the entire argument for this file. `-derive` was correct and the
// playground would still have shipped six of seven projects, with a valid
// manifest, a page that renders and nothing red, until somebody happened to
// notice simdjson missing. No test written against a fixture could have seen
// it.
//
// ⛔ AND THE GROUND TRUTH IS FETCHED, NOT COPIED. A set of seven names pasted
// into this file would be a hand-typed list guarding a hand-typed list — the
// exact defect `-derive` exists to remove. The lane files are read over the
// network, at the version go-pkgx/packages serves now. The names above are a
// dated observation for a human reader, and nothing compares against them.
//
// ⛔ WHICH IS WHY THIS NEEDS A NETWORK, AND SAYS SO. It is skipped unless
// WASMSTAGE_CONFRONT=1, and the workflow sets that only in the scheduled run —
// the same argument the workflow header makes about `schedule`: the event that
// changes this answer happens in a different repository, so running it on
// every push would be both useless and fragile.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// laneFiles are the factory workflows that declare what is built for
// wasip1/wasm. Read from the default branch, because that is what the next
// scheduled run of the factory will use.
var laneFiles = []string{
	"https://raw.githubusercontent.com/go-pkgx/packages/main/.github/workflows/wasip1.yml",
	"https://raw.githubusercontent.com/go-pkgx/packages/main/.github/workflows/wasip1-c.yml",
}

// lane is the part of a factory workflow this reads: every job's matrix
// include entries, of which only the slug matters here.
//
// Parsed as YAML rather than matched with an expression. A `slug:` seen by a
// pattern is a string that appeared in a file; a slug read through the
// document structure is one the workflow will actually expand into a build.
type lane struct {
	Jobs map[string]struct {
		Strategy struct {
			Matrix struct {
				Include []struct {
					Slug string `yaml:"slug"`
				} `yaml:"include"`
			} `yaml:"matrix"`
		} `yaml:"strategy"`
	} `yaml:"jobs"`
}

// declaredBy reads one lane file and returns the projects it builds.
//
// ⛔ ZERO IS AN ERROR, NEVER AN ANSWER. If the job is renamed, the matrix
// moves, or `slug` becomes `project`, this parse yields an empty set — and an
// empty declared set makes the comparison below pass for every possible
// derived list. A scan that could not read must not report nothing found.
func declaredBy(url string) ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	// Bounded: a workflow is a few kilobytes, and a test must not be the
	// thing that decides how much of a redirected body to hold.
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var l lane
	if err := yaml.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	seen := map[string]bool{}
	var slugs []string
	for _, j := range l.Jobs {
		for _, e := range j.Strategy.Matrix.Include {
			// The C lane lists each project once per HOST runner
			// (linux/x86-64 and linux/aarch64) and publishes both as
			// wasip1/wasm, so the same slug appears twice.
			if e.Slug != "" && !seen[e.Slug] {
				seen[e.Slug] = true
				slugs = append(slugs, e.Slug)
			}
		}
	}
	if len(slugs) == 0 {
		return nil, fmt.Errorf("%s declares no matrix slug — the file was read and understood as nothing, which cannot be right for a lane that publishes bottles; the workflow's shape has probably changed", url)
	}
	sort.Strings(slugs)
	return slugs, nil
}

// missingFrom is the members of want that have is not.
func missingFrom(want, is []string) []string {
	has := map[string]bool{}
	for _, s := range is {
		has[s] = true
	}
	var out []string
	for _, s := range want {
		if !has[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// ⛔ THE COMPARISON ITSELF HAS A UNIT TEST, because the confrontation below
// needs a network and will not run on most changes. A `missingFrom` that
// always returned nil would make that test green forever against anything.
func TestTheComparisonCanSeeADisagreement(t *testing.T) {
	declared := []string{"a.org", "b.org", "c.org"}
	for _, c := range []struct {
		what                string
		derived             []string
		wantMiss, wantExtra []string
	}{
		{"they agree", []string{"a.org", "b.org", "c.org"}, nil, nil},
		{"a silent subset — the failure mode this exists for", []string{"a.org", "c.org"}, []string{"b.org"}, nil},
		{"the catalogue knows one the lanes do not", []string{"a.org", "b.org", "c.org", "d.org"}, nil, []string{"d.org"}},
		{"both directions at once", []string{"a.org", "d.org"}, []string{"b.org", "c.org"}, []string{"d.org"}},
		{"nothing derived at all", nil, declared, nil},
	} {
		miss := missingFrom(declared, c.derived)
		extra := missingFrom(c.derived, declared)
		if strings.Join(miss, ",") != strings.Join(c.wantMiss, ",") {
			t.Errorf("%s: missing = %v, want %v", c.what, miss, c.wantMiss)
		}
		if strings.Join(extra, ",") != strings.Join(c.wantExtra, ",") {
			t.Errorf("%s: extra = %v, want %v", c.what, extra, c.wantExtra)
		}
	}
}

// The confrontation. The published catalogue is asked what exists; the factory
// is asked what it builds; the two sets must be the same set.
func TestTheDerivedListMatchesWhatTheFactoryBuilds(t *testing.T) {
	if os.Getenv("WASMSTAGE_CONFRONT") != "1" {
		t.Skip("set WASMSTAGE_CONFRONT=1 — this one reaches the registry and go-pkgx/packages")
	}

	var declared []string
	for _, u := range laneFiles {
		d, err := declaredBy(u)
		if err != nil {
			t.Fatalf("the ground truth could not be read, so nothing was compared: %v", err)
		}
		t.Logf("%s declares %d: %s", u[strings.LastIndex(u, "/")+1:], len(d), strings.Join(d, " "))
		declared = append(declared, d...)
	}
	sort.Strings(declared)

	// The real catalogue, through the real fetch: the seam is deliberately
	// not replaced here, because a confrontation against a fixture is not a
	// confrontation.
	cat, err := fetchCatalog(stageOS, stageArch)
	if err != nil {
		t.Fatalf("the %s/%s catalogue could not be read: %v\n"+
			"If this is \"platform not in index\", go-pkgx/packages' catalogue job has not published this platform yet — wait for it. That is a MISSING measurement, not a passing one, which is why this is a failure and not a skip.",
			stageOS, stageArch, err)
	}
	derived, err := derive(stageOS, stageArch)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	missing := missingFrom(declared, derived)
	extra := missingFrom(derived, declared)
	if len(missing) == 0 && len(extra) == 0 {
		t.Logf("the catalogue of %s and the two lane files agree on %d project(s): %s",
			cat.Generated, len(derived), strings.Join(derived, " "))
		return
	}

	// ⛔ NAME THE DIRECTION AND THE DISCRIMINATOR. The two sides can disagree
	// legitimately, and the reader cannot tell which case they are in from a
	// bare "not equal".
	var b strings.Builder
	fmt.Fprintf(&b, "the derived list and the factory's lanes disagree.\n")
	// Age() already ends in "old"; do not say it twice.
	fmt.Fprintf(&b, "  catalogue generated %s (%s)\n", cat.Generated, cat.Age(time.Now()))
	fmt.Fprintf(&b, "  the factory declares %d: %s\n", len(declared), strings.Join(declared, " "))
	fmt.Fprintf(&b, "  the catalogue yields %d: %s\n", len(derived), strings.Join(derived, " "))
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\n  BUILT BUT NOT DERIVED: %s\n", strings.Join(missing, " "))
		fmt.Fprintf(&b, "  Two explanations, and the catalogue's timestamp above tells them apart:\n")
		fmt.Fprintf(&b, "   - the lane added it AFTER that timestamp. Benign; the next weekly catalogue\n")
		fmt.Fprintf(&b, "     run picks it up, and the playground is one week behind until then.\n")
		fmt.Fprintf(&b, "   - the lane has been building it since BEFORE that timestamp. Then the bottle\n")
		fmt.Fprintf(&b, "     or its catalogue entry is missing, and the playground is silently short a\n")
		fmt.Fprintf(&b, "     project — which is the failure this test was written for.\n")
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "\n  DERIVED BUT NOT DECLARED: %s\n", strings.Join(extra, " "))
		fmt.Fprintf(&b, "  The registry publishes it for %s/%s and neither lane file lists it. Either a\n", stageOS, stageArch)
		fmt.Fprintf(&b, "  third lane file now exists -- add it to laneFiles, this list is the ground\n")
		fmt.Fprintf(&b, "  truth and must stay complete -- or something was published by hand.\n")
		fmt.Fprintf(&b, "  Staging it is not wrong; not knowing where it came from is.\n")
	}
	t.Error(b.String())
}
