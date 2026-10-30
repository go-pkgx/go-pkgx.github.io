// Runs one wasip1 module through static/playground/wasi.js — the page's own
// shim, not a copy of it — and prints what came out as JSON.
//
// It exists so the shim can be CONFRONTED with wasmtime. A browser cannot be
// driven from here, but the syscall layer is the part that can be wrong in a
// way nobody notices, and it is plain JavaScript over an ArrayBuffer: given a
// JS runtime, the same file that the page loads can be run against the same
// modules with the same arguments, and its stdout, stderr and exit status
// compared byte for byte with the reference's.
//
// The Go test copies wasi.js next to this file as `wasi.mjs` (Node decides a
// file's module kind by extension, and the page needs the .js name), so the
// import below is of the published file, unmodified.

import { readFile } from "node:fs/promises";
import { WASI, MemFS } from "./wasi.mjs";

function fail(message) {
  process.stdout.write(JSON.stringify({ error: message }) + "\n");
  process.exit(1);
}

const specPath = process.argv[2];
if (!specPath) fail("usage: runshim.mjs <spec.json>");

const spec = JSON.parse(await readFile(specPath, "utf8"));

const fs = new MemFS();
// The working directory has to EXIST before the guest tries to create a file
// in it: $PWD is how Go's wasip1 runtime resolves a relative path against a
// preopen, and an absent directory would read as "the file is missing".
if (spec.env && spec.env.PWD) fs.mkdirp(spec.env.PWD);
for (const [name, b64] of Object.entries(spec.files ?? {})) {
  fs.writeFile(name, Buffer.from(b64, "base64"));
}

const wasi = new WASI({
  args: spec.argv ?? [],
  env: spec.env ?? {},
  stdin: spec.stdin ? Buffer.from(spec.stdin, "base64") : new Uint8Array(0),
  fs,
});

let result;
try {
  const bytes = await readFile(spec.module);
  const { instance } = await WebAssembly.instantiate(bytes, wasi.imports);
  result = wasi.start(instance);
} catch (err) {
  // A trap is a result too, and a different one from a non-zero exit: say
  // which it was instead of letting both read as "it did not work".
  process.stdout.write(
    JSON.stringify({
      trap: String(err && err.stack ? err.stack : err),
      stdout: Buffer.concat(wasi.stdout.map(Buffer.from)).toString("base64"),
      stderr: Buffer.concat(wasi.stderr.map(Buffer.from)).toString("base64"),
      diagnostics: wasi.diagnostics,
    }) + "\n",
  );
  process.exit(0);
}

process.stdout.write(
  JSON.stringify({
    exit: result.exitCode,
    stdout: Buffer.from(result.stdout).toString("base64"),
    stderr: Buffer.from(result.stderr).toString("base64"),
    diagnostics: result.diagnostics,
  }) + "\n",
);
