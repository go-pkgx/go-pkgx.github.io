---
title: "Run the wasip1 bottles here"
layout: playground
---

The factory publishes a `wasip1/wasm` lane: pure-Go tools, and C and C++ tools built
inside the sovereign rootfs with our own source-built toolchain. A `wasm32-wasip1`
module is **statically linked** — there is no closure left to resolve — so the only
thing between one of those bottles and your browser is a host for the thirty-odd
syscalls it makes. That host is [`wasi.js`](wasi.js), about seven hundred lines, written
against the preview1 specification and **confronted with `wasmtime`**: all eight modules
below are run under the reference implementation in CI, their stdout, stderr and exit
status recorded as fixtures, and the same modules run again through this page's own shim
with the bytes required to match. One difference that found — sqlite3 printing a
box-drawing table where wasmtime printed `3.53.4` — was a single wrong bit in what
`fd_fdstat_get` reports about stdout.

Nothing here is fetched from a registry: `ghcr.io` sends no CORS headers, so the
modules are staged into this site during the build and served from it.

**What this host does not do is written next to the output, not hidden.** A syscall it
does not implement returns `ENOSYS` and names itself in a panel below — never 0, because
a run that silently skipped a call and exited cleanly looks exactly like one that worked.
It does not sleep (`poll_oneoff` returns a timer as already elapsed), it has no network,
and its filesystem lives in memory for the length of one run.
