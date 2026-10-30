// The playground page: pick one of the staged wasip1/wasm modules, edit its
// arguments and its input, run it here, read what it printed.
//
// ⛔ IT NEVER TALKS TO A REGISTRY. ghcr.io sends no CORS headers, so a page
// that fetched a bottle would fail in every browser and pass in every test
// written outside one. Everything below is same-origin, written by
// tools/wasmstage in CI.
//
// ⛔ AND IT NEVER PRETENDS THERE IS NOTHING TO SHOW. A missing index.json means
// the staging step did not run; that is a broken build, not an empty platform,
// and the page says which.

import { WASI, MemFS, WASIExit } from "./wasi.js";
import { PRESETS } from "./presets.js";

const el = (id) => document.getElementById(id);

/** Where the guest believes it is, and where the editable files are put. */
const CWD = "/smoke";

let manifest = null;
let current = null;

// Guarded so this module can also be imported by a test runner, which has no
// document and no page to fill.
if (typeof document !== "undefined") init();

async function init() {
  const root = el("playground");
  const base = root.dataset.modules;
  try {
    const res = await fetch(base + "index.json", { cache: "no-cache" });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    manifest = await res.json();
  } catch (err) {
    absent(base, err);
    return;
  }
  if (!manifest.modules || manifest.modules.length === 0) {
    absent(base, new Error("the manifest lists no modules"));
    return;
  }
  el("pg-missing").hidden = true;
  el("pg-ui").hidden = false;
  el("pg-generated").textContent = manifest.generated
    ? `${manifest.platform} — staged ${manifest.generated}`
    : manifest.platform;

  const picker = el("pg-module");
  for (const m of manifest.modules) {
    const opt = document.createElement("option");
    opt.value = m.path;
    opt.textContent = `${m.bin} — ${m.project} ${m.version}`;
    picker.append(opt);
  }
  picker.addEventListener("change", () => select(picker.value));
  el("pg-run").addEventListener("click", run);
  el("pg-reset").addEventListener("click", () => select(picker.value));
  select(manifest.modules[0].path);
}

function absent(base, err) {
  el("pg-ui").hidden = true;
  const box = el("pg-missing");
  box.hidden = false;
  el("pg-missing-why").textContent = String(err && err.message ? err.message : err);
  el("pg-missing-url").textContent = base + "index.json";
}

function select(path) {
  current = manifest.modules.find((m) => m.path === path);
  el("pg-module").value = path;
  const preset = PRESETS[current.bin] ?? { args: [] };
  el("pg-argv").value = [current.bin, ...preset.args].map(quote).join(" ");
  el("pg-stdin").value = preset.stdin ?? "";
  const files = preset.files ?? {};
  const names = Object.keys(files);
  el("pg-file-box").hidden = names.length === 0;
  if (names.length > 0) {
    el("pg-file-name").value = names[0];
    el("pg-file-body").value = files[names[0]];
  }
  el("pg-size").textContent = `${current.bytes.toLocaleString("en-US")} bytes`;
  el("pg-sha").textContent = current.sha256;
  clearOutput();
}

function clearOutput() {
  el("pg-stdout").textContent = "";
  el("pg-stderr").textContent = "";
  el("pg-status").textContent = "";
  el("pg-status").className = "pg-status";
  el("pg-diag").textContent = "";
  el("pg-diag-box").hidden = true;
  el("pg-download").hidden = true;
}

async function run() {
  if (!current) return;
  clearOutput();
  const button = el("pg-run");
  button.disabled = true;
  status("loading…", "");
  try {
    const bytes = await load(current);
    const fs = new MemFS();
    fs.mkdirp(CWD);
    if (!el("pg-file-box").hidden) {
      const name = el("pg-file-name").value.trim();
      if (name) fs.writeFile(`${CWD}/${name}`, el("pg-file-body").value);
    }
    const argv = tokenise(el("pg-argv").value);
    if (argv.length === 0) {
      status("nothing to run: argv is empty", "bad");
      return;
    }
    const wasi = new WASI({
      args: argv,
      // $PWD is how Go's wasip1 runtime decides which preopen its working
      // directory is in; the C modules resolve a relative path the same way.
      env: { PWD: CWD },
      stdin: el("pg-stdin").value,
      fs,
    });

    status("running…", "");
    // Synchronous on purpose: a wasip1 command is a straight line from _start
    // to proc_exit, and there is nothing to await in the middle. It does hold
    // the page while it runs.
    const { instance } = await WebAssembly.instantiate(bytes, wasi.imports);
    const result = wasi.start(instance);
    render(result);
  } catch (err) {
    if (err instanceof WASIExit) {
      status(`exit ${err.code}`, err.code === 0 ? "ok" : "bad");
    } else {
      // A trap is not an exit status: say which happened.
      status(`trapped: ${err && err.message ? err.message : err}`, "bad");
      el("pg-stderr").textContent = String(err && err.stack ? err.stack : err);
    }
  } finally {
    button.disabled = false;
  }
}

/** Fetch the module and check it against the SHA-256 the manifest recorded. */
async function load(m) {
  const base = el("playground").dataset.modules;
  const res = await fetch(base + m.path, { cache: "force-cache" });
  if (!res.ok) throw new Error(`${m.path}: ${res.status} ${res.statusText}`);
  const bytes = new Uint8Array(await res.arrayBuffer());
  if (crypto?.subtle?.digest) {
    const digest = await crypto.subtle.digest("SHA-256", bytes);
    const hex = [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
    if (hex !== m.sha256) {
      // The manifest and the file are served from the same origin by the same
      // build, so a mismatch is a broken deploy — worth stopping for rather
      // than running a module nobody recorded.
      throw new Error(`${m.path}: sha256 ${hex} is not the manifest's ${m.sha256}`);
    }
  }
  return bytes;
}

function render(result) {
  const dec = new TextDecoder("utf-8", { fatal: false });
  if (isPrintable(result.stdout)) {
    el("pg-stdout").textContent = dec.decode(result.stdout);
  } else {
    el("pg-stdout").textContent =
      `${result.stdout.length} bytes of binary output\n\n` + hexdump(result.stdout.subarray(0, 256));
    const link = el("pg-download");
    link.href = URL.createObjectURL(new Blob([result.stdout], { type: "application/octet-stream" }));
    link.download = `${current.bin}.out`;
    link.hidden = false;
  }
  el("pg-stderr").textContent = dec.decode(result.stderr);
  status(`exit ${result.exitCode}`, result.exitCode === 0 ? "ok" : "bad");

  if (result.diagnostics.length > 0) {
    // ⛔ The point of the whole shim: a syscall it does not implement is said
    // out loud, next to the output, so nobody reads a partial run as a result.
    el("pg-diag-box").hidden = false;
    el("pg-diag").textContent = result.diagnostics
      .map((d) => `${d.name} ×${d.count} — ${d.detail}`)
      .join("\n");
  }
}

function status(text, kind) {
  const node = el("pg-status");
  node.textContent = text;
  node.className = "pg-status" + (kind ? " " + kind : "");
}

function isPrintable(bytes) {
  for (const c of bytes) {
    if (c === 0x09 || c === 0x0a || c === 0x0d) continue;
    if (c < 0x20 || c === 0x7f) return false;
  }
  return true;
}

function hexdump(bytes) {
  const lines = [];
  for (let i = 0; i < bytes.length; i += 16) {
    const row = bytes.subarray(i, i + 16);
    const hex = [...row].map((b) => b.toString(16).padStart(2, "0")).join(" ");
    const txt = [...row].map((b) => (b >= 0x20 && b < 0x7f ? String.fromCharCode(b) : ".")).join("");
    lines.push(`${i.toString(16).padStart(8, "0")}  ${hex.padEnd(47)}  ${txt}`);
  }
  return lines.join("\n");
}

/**
 * Split a command line the way a shell would, for the quoting these presets
 * need and no more: single quotes, double quotes, and a backslash escape.
 * It is NOT a shell — there is no expansion, no globbing and no pipeline — and
 * it says so rather than half-doing it.
 */
export function tokenise(line) {
  const out = [];
  let cur = "";
  let quote = null;
  let any = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (quote === null && (c === " " || c === "\t" || c === "\n")) {
      if (any) out.push(cur);
      cur = "";
      any = false;
      continue;
    }
    if (c === "\\" && quote !== "'" && i + 1 < line.length) {
      cur += line[++i];
      any = true;
      continue;
    }
    if (quote === null && (c === '"' || c === "'")) {
      quote = c;
      any = true;
      continue;
    }
    if (c === quote) {
      quote = null;
      continue;
    }
    cur += c;
    any = true;
  }
  if (any) out.push(cur);
  return out;
}

/** The inverse of tokenise, for filling the box from a preset. */
export function quote(arg) {
  if (arg === "") return '""';
  if (!/[\s"'\\]/.test(arg)) return arg;
  return '"' + arg.replace(/(["\\])/g, "\\$1") + '"';
}
