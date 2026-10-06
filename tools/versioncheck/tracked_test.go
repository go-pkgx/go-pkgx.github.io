package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The magic numbers of the executable formats a Go build can produce. Only
// executables: an SVG or a PNG under static/ is legitimate, a compiled command
// is not, and a check that cannot tell them apart is one somebody switches off.
var executableMagic = []struct {
	name  string
	magic []byte
}{
	{"ELF", []byte{0x7f, 'E', 'L', 'F'}},
	{"Mach-O 64-bit little-endian", []byte{0xcf, 0xfa, 0xed, 0xfe}},
	{"Mach-O 32-bit little-endian", []byte{0xce, 0xfa, 0xed, 0xfe}},
	{"Mach-O big-endian", []byte{0xfe, 0xed, 0xfa, 0xcf}},
	{"Mach-O universal", []byte{0xca, 0xfe, 0xba, 0xbe}},
	{"PE/COFF", []byte{'M', 'Z'}},
	{"WebAssembly", []byte{0x00, 'a', 's', 'm'}},
}

// No compiled binary may be tracked. `go build` here writes ./versioncheck, and
// 8 887 250 bytes of Mach-O arm64 were tracked from #16 -- the very change that
// added this tool -- until the commit that added this test, because the
// .gitignore said nothing about it and `git add -A` does not ask.
//
// The allow-list fixed in the same commit cannot go stale at a rename. This is
// the other half: `git add -f` walks straight past a .gitignore, and this says
// so for the whole tree rather than for one directory, failing with the path
// and the format rather than with a size.
func TestNoTrackedFileIsACompiledBinary(t *testing.T) {
	root := "../.."
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files: %v (not a checkout?)", err)
	}
	names := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")

	var checked int
	var found []string
	for _, n := range names {
		if n == "" {
			continue
		}
		f, err := os.Open(filepath.Join(root, n))
		if err != nil {
			continue // tracked but not checked out here
		}
		head := make([]byte, 4)
		k, _ := f.Read(head)
		f.Close()
		checked++
		for _, m := range executableMagic {
			if k >= len(m.magic) && bytes.HasPrefix(head, m.magic) {
				found = append(found, n+" ("+m.name+")")
				break
			}
		}
	}

	// A sweep that could not read reports zero: prove this one read something.
	if checked < 10 {
		t.Fatalf("read %d tracked files, so this test is measuring itself", checked)
	}
	if len(found) > 0 {
		t.Fatalf("%d tracked file(s) are compiled binaries: %v\n"+
			"`go build` in tools/<name>/ writes tools/<name>/<name>; the allow-list in\n"+
			".gitignore keeps it out, so this one predates that rule or went past it\n"+
			"with `git add -f`.", len(found), found)
	}
	t.Logf("%d tracked files, none is an executable", checked)
}
