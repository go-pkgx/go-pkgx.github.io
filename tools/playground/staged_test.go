package playground

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// stagingWorkflows are every file that stages modules, and they must all name
// the same set.
//
// There were three by 2026-10-31 and only one was checked. `hugo.yml` and
// `playground.yml` used `wasmstage -derive`, which was right about rot and
// wrong about what to stage: when the lane grew from 7 projects to 34, a
// derived run staged 35 modules weighing 441 MB, of which only eight had been
// confronted with wasmtime. They now name the confronted set, which means
// three lists exist and all three can drift.
var stagingWorkflows = []string{
	repoRoot + "/.github/workflows/shim.yml",
	repoRoot + "/.github/workflows/hugo.yml",
	repoRoot + "/.github/workflows/playground.yml",
}

// TestTheStagedProjectsAreTheConfrontedOnes refuses to let the workflow's
// project names become a SECOND list.
//
// The names in shim.yml and the `project` fields in the fixtures have to be the
// same set, and nothing in CI would say so otherwise. A fixture recorded for a
// ninth module, with no name added to the workflow, fails on a missing file in
// the staged tree — a message about a path, several steps later, that reads
// like a staging bug. A name in the workflow with no fixture behind it is
// worse: 21 MB is pulled, every assertion passes, and the module it was pulled
// for is never run. Neither is visible as what it is.
//
// So the workflow is parsed, not grepped: `run:` scripts are shell, and a
// `go run .` inside a comment or in another step is not the staging step. And
// an empty result is an error, because a set that parsed to nothing would agree
// with every possible fixture file.
func TestTheStagedProjectsAreTheConfrontedOnes(t *testing.T) {
	f, err := LoadFixtures(fixturesPath)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, c := range f.Cases {
		if c.Project == "" {
			t.Fatalf("case %q names no project", c.Name)
		}
		want[c.Project] = true
	}

	for _, wf := range stagingWorkflows {
		got := map[string]bool{}
		for _, p := range projectsStagedBy(t, wf) {
			got[p] = true
		}

		for p := range want {
			if !got[p] {
				t.Errorf("%s: the fixtures confront %s and this workflow does not stage it: "+
					"add it to the staging step", wf, p)
			}
		}
		for p := range got {
			if !want[p] {
				t.Errorf("%s: this workflow stages %s and no fixture runs it — so the page "+
					"would offer a module whose behaviour was never compared with wasmtime. "+
					"Record a fixture with `go test ./tools/playground -update`, or drop the name",
					wf, p)
			}
		}
		if t.Failed() {
			t.Logf("fixtures:    %v", sortedKeys(want))
			t.Logf("%s: %v", wf, sortedKeys(got))
			return
		}
	}
}

// projectsStagedBy reads the workflow and returns the project names its
// staging step passes to wasmstage.
func projectsStagedBy(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("%s: %v", path, err)
	}

	var scripts []string
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "go run .") && strings.Contains(step.Run, "-out") {
				scripts = append(scripts, step.Run)
			}
		}
	}
	if len(scripts) != 1 {
		t.Fatalf("%s: found %d staging step(s), expected exactly 1: "+
			"this test cannot tell which list is the one the confrontation runs on",
			path, len(scripts))
	}

	// Everything the invocation itself is made of. What remains is a project,
	// and an unrecognised token is an ERROR rather than something skipped: a
	// flag quietly dropped here is a flag whose effect nobody is checking.
	boilerplate := map[string]bool{
		"go": true, "run": true, ".": true, "-out": true, `\`: true,
	}
	var projects []string
	for _, tok := range strings.Fields(scripts[0]) {
		if boilerplate[tok] {
			continue
		}
		if strings.HasPrefix(tok, `"`) || strings.HasPrefix(tok, "$") {
			continue // the -out path
		}
		if strings.HasPrefix(tok, "-") {
			t.Fatalf("%s: unrecognised flag %q in the staging step: reread this test", path, tok)
		}
		projects = append(projects, tok)
	}
	if len(projects) == 0 {
		t.Fatalf("%s: the staging step names no project at all", path)
	}
	return projects
}

// fixtureProjects is the set of projects the fixtures confront, sorted.
func fixtureProjects(t *testing.T) []string {
	t.Helper()
	f, err := LoadFixtures(fixturesPath)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range f.Cases {
		seen[c.Project] = true
	}
	return sortedKeys(seen)
}

// TestEveryStagingWorkflowIsChecked guards the list above, which is the one
// thing in this file that a new workflow can silently fall outside of. A fourth
// workflow staging modules with nothing asserting its list is exactly the hole
// `hugo.yml` and `playground.yml` sat in while only `shim.yml` was checked.
func TestEveryStagingWorkflowIsChecked(t *testing.T) {
	dir := filepath.Join(repoRoot, ".github", "workflows")
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	for _, w := range stagingWorkflows {
		checked[filepath.Base(w)] = true
	}
	found := 0
	for _, n := range names {
		if n.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "go run . -out") {
			continue
		}
		found++
		if !checked[n.Name()] {
			t.Errorf("%s stages modules and is not in stagingWorkflows: its project list "+
				"is unchecked, which is how hugo.yml and playground.yml drifted to -derive",
				n.Name())
		}
	}
	if found != len(stagingWorkflows) {
		t.Errorf("found %d workflow(s) with a staging step, stagingWorkflows names %d: %v",
			found, len(stagingWorkflows), stagingWorkflows)
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
