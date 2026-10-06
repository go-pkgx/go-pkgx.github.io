// Command versioncheck fails when a version written on this site has fallen
// behind the release it names.
//
// Why this exists. The install section argues, deliberately, that a one-liner
// should name a version: "a line copied today and the same line copied in six
// months install the same bytes". That is a good argument and it is also a
// maintenance obligation, and nobody met it. On 2026-10-04 the page told
// visitors to install pkgm v0.1.4 and pkgx v0.1.5 while the releases were
// v0.1.7 and v0.1.10 — three and five behind, and both predating two directory
// traversal fixes in the shared tar extractor (go-pkgx/bottle#106, #107). A
// pinned example that rots does not merely go stale: it hands people an old
// build, which is worse than the unpinned form the installer already supports.
//
// Why it is SCHEDULED and not a build step. The site is built on push to this
// repository and on pull requests. A check that runs only then cannot see a
// release published in a DIFFERENT repository, which is the only way this ever
// goes wrong. A guard has to watch the event that actually changes the answer.
//
// It authenticates with nothing. Three unauthenticated calls a day sit far
// under GitHub's 60/hour for anonymous clients, and a token this does not hold
// is a token that cannot leak.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// pin is one version string found in the tree.
type pin struct {
	file string
	line int
	tool string
	ver  string
}

// Two shapes carry a version here: the positional form the shell installer
// takes, and the environment variable PowerShell needs because a piped script
// gets no arguments.
var (
	rePositional = regexp.MustCompile(`\b(pkgm|pkgx|mirror)\s+(v\d+\.\d+\.\d+)\b`)
	reEnvVar     = regexp.MustCompile(`\b(PKGM|PKGX|MIRROR)_VERSION\s*=\s*'?(v\d+\.\d+\.\d+)'?`)
)

// scanned are the files a reader can act on. A version inside a CSS comment is
// describing something that was once measured, not telling anybody what to
// install, so layouts/partials is deliberately absent.
var scanned = []string{"layouts/index.html", "content", "static/install.sh", "static/install.ps1"}

func main() {
	// The module lives in tools/versioncheck and the files it reads live at
	// the repository root, so the root is a flag rather than the working
	// directory: `go run ./tools/versioncheck` from the root cannot work when
	// the root is not itself a module.
	root := flag.String("root", "../..", "the site checkout to read")
	flag.Parse()
	if err := run(os.DirFS(*root), latestRelease, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run is main with its two edges injected, so the whole thing is testable
// without a network and without a checkout.
func run(root fs.FS, latest func(tool string) (string, error), out io.Writer) error {
	pins, err := collect(root)
	if err != nil {
		return err
	}
	// A scan that finds nothing reports success exactly like one that found
	// everything in order. Say the count, and treat zero as a failure: it
	// means the layout moved, not that the site is clean.
	if len(pins) == 0 {
		return fmt.Errorf("versioncheck: no version found in %s — the layout changed", strings.Join(scanned, ", "))
	}

	tools := map[string]bool{}
	for _, p := range pins {
		tools[p.tool] = true
	}
	names := make([]string, 0, len(tools))
	for t := range tools {
		names = append(names, t)
	}
	sort.Strings(names)

	current := map[string]string{}
	for _, t := range names {
		v, err := latest(t)
		if err != nil {
			return fmt.Errorf("versioncheck: %s: %w", t, err)
		}
		current[t] = v
	}

	var behind []string
	for _, p := range pins {
		if semver.Compare(p.ver, current[p.tool]) < 0 {
			behind = append(behind, fmt.Sprintf("  %s:%d: %s %s — released %s", p.file, p.line, p.tool, p.ver, current[p.tool]))
		}
	}
	fmt.Fprintf(out, "%d version(s) checked against %d release(s)\n", len(pins), len(names))
	for _, t := range names {
		fmt.Fprintf(out, "  %s %s\n", t, current[t])
	}
	if len(behind) > 0 {
		sort.Strings(behind)
		return fmt.Errorf("versioncheck: %d pinned version(s) behind their release:\n%s\n\nThe install section names a version on purpose; that is a promise to keep it current.", len(behind), strings.Join(behind, "\n"))
	}
	return nil
}

// collect reads every scanned path and returns each version it mentions.
func collect(root fs.FS) ([]pin, error) {
	var pins []pin
	for _, target := range scanned {
		err := fs.WalkDir(root, target, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				// A path that is not there is not a failure: the set above is
				// a superset so that adding a page does not need a code change.
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			b, err := fs.ReadFile(root, p)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(b), "\n") {
				for _, m := range rePositional.FindAllStringSubmatch(line, -1) {
					pins = append(pins, pin{p, i + 1, m[1], m[2]})
				}
				for _, m := range reEnvVar.FindAllStringSubmatch(line, -1) {
					pins = append(pins, pin{p, i + 1, strings.ToLower(m[1]), m[2]})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return pins, nil
}

// apiBase is where the release lookup goes. A variable so a test can point it
// at an httptest server; nothing else changes it.
var apiBase = "https://api.github.com"

// latestRelease asks GitHub for a repository's newest release tag.
//
// It authenticates when the environment offers a token. Unauthenticated the
// limit is 60 requests an hour PER IP, shared with every other job on that
// runner, and this workflow runs on every pull request and every push as well
// as on its daily cron -- so the lane goes red for a reason that has nothing
// to do with a version. With GITHUB_TOKEN the limit is 1 000 an hour for the
// repository. The token is only ever put in a header; it is never printed,
// including in the errors below.
func latestRelease(tool string) (string, error) {
	c := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, apiBase+"/repos/go-pkgx/"+tool+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "go-pkgx-versioncheck")
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// ⛔ A call that did not happen must not read as a verdict about a
		// version. A 403 with no requests left is the rate limit, and saying
		// "GET releases/latest: 403 Forbidden" beside a list of pinned
		// versions invites exactly the wrong conclusion -- that something is
		// out of date -- when in fact NOTHING was compared.
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			if resp.Header.Get("X-RateLimit-Remaining") == "0" {
				return "", fmt.Errorf("rate limited by GitHub (%s), so NO version was compared%s; "+
					"set GITHUB_TOKEN to raise the limit from 60/hour per IP to 1000/hour",
					resp.Status, resetHint(resp.Header.Get("X-RateLimit-Reset")))
			}
		}
		return "", fmt.Errorf("GET releases/latest: %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if !semver.IsValid(body.TagName) {
		return "", fmt.Errorf("latest release is %q, which is not a semver tag", body.TagName)
	}
	return body.TagName, nil
}

// githubToken reads the token CI provides, without ever logging it. GH_TOKEN
// is what the gh CLI sets, GITHUB_TOKEN what Actions sets; either will do.
func githubToken() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// resetHint turns the rate-limit reset header into something a person can act
// on. An unparseable or absent value yields nothing rather than a wrong time.
func resetHint(epoch string) string {
	if epoch == "" {
		return ""
	}
	n, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		return ""
	}
	return ", which resets at " + time.Unix(n, 0).UTC().Format("15:04:05Z")
}
