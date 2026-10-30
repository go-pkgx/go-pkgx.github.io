// Command probe is a planted positive for the playground's WASI shim.
//
// It exercises the syscalls the shim claims to implement — argv, the
// environment, stdin, path_open, fd_write, fd_seek, fd_read, path_filestat_get
// — and then one it deliberately does NOT: os.Symlink, which lowers to
// path_symlink. A test that only ever saw calls succeed could not tell a
// working diagnostic channel from a silent one, so this makes the failure
// happen on purpose and prints what came back.
//
// It is built for wasip1/wasm by the test, never committed as a .wasm.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	fmt.Printf("argv: %v\n", os.Args)
	fmt.Printf("pwd: %s\n", os.Getenv("PWD"))

	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Printf("stdin: error %v\n", err)
	} else {
		fmt.Printf("stdin: %d bytes\n", len(in))
	}

	const name = "probe.txt"
	if err := os.WriteFile(name, []byte("written by the probe\n"), 0o644); err != nil {
		fmt.Printf("write: error %v\n", err)
	} else {
		back, err := os.ReadFile(name)
		fmt.Printf("read back: %q err=%v\n", string(back), err)
	}
	if st, err := os.Stat(name); err == nil {
		fmt.Printf("stat: %d bytes\n", st.Size())
	} else {
		fmt.Printf("stat: error %v\n", err)
	}

	// ⛔ The planted one. path_symlink is NOT implemented, and the shim must
	// make that visible rather than pretend it worked.
	if err := os.Symlink(name, "probe.link"); err != nil {
		fmt.Printf("symlink: error %v\n", err)
	} else {
		fmt.Printf("symlink: ok\n")
	}

	fmt.Fprintln(os.Stderr, "probe: done")
}
