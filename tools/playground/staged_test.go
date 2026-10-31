package playground

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// shimWorkflow is the file that stages the modules this package confronts.
const shimWorkflow = repoRoot + "/.github/workflows/shim.yml"

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

	got := map[string]bool{}
	for _, p := range projectsStagedBy(t, shimWorkflow) {
		got[p] = true
	}

	for p := range want {
		if !got[p] {
			t.Errorf("the fixtures confront %s and the workflow does not stage it: "+
				"add it to the `stage the modules the fixtures name` step in %s",
				p, shimWorkflow)
		}
	}
	for p := range got {
		if !want[p] {
			t.Errorf("the workflow stages %s and no fixture runs it: "+
				"record one with `go test ./tools/playground -update`, or drop the name",
				p)
		}
	}
	if t.Failed() {
		t.Logf("fixtures: %v", sortedKeys(want))
		t.Logf("workflow: %v", sortedKeys(got))
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

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
