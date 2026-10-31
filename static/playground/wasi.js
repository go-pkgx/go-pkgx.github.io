// A WASI preview1 host, in the browser.
//
// # Where every number in this file comes from
//
// Nothing here is from memory. The three documents that were opened, and what
// each one settles:
//
//   - WebAssembly/WASI, branch `wasi-0.1`, `preview1/docs.md` — the TYPES: the
//     ordinal of every `errno` case, the offsets inside `fdstat`, `filestat`,
//     `prestat`, `iovec`, `subscription` and `event`, and the bit numbers of
//     `oflags`, `fdflags` and `lookupflags`. (`legacy/preview1/docs.md` on
//     `main` is a 404 today; the document lives on the `wasi-0.1` branch.)
//   - WebAssembly/wasi-libc, `libc-bottom-half/sources/__wasilibc_real.c` — the
//     CORE SIGNATURES. docs.md writes `path_open(…, path: string, …)`; the
//     lowering of `string` to a (pointer, length) PAIR, and of
//     `Result<T, errno>` to "return the errno, write T through a trailing
//     pointer", is only visible in the real import declarations there.
//   - WebAssembly/wasi-libc, `libc-bottom-half/headers/public/wasi/wasip1.h` —
//     the same constants a second time, as a CONTROL. `__WASI_ERRNO_NOSYS
//     (UINT16_C(52))` agreeing with the 52nd bullet of docs.md is what says the
//     ordinals were counted correctly rather than confidently.
//   - WebAssembly/WASI, `application-abi.md` — `_start`, the exported `memory`,
//     and that fds 0, 1 and 2 are provided before `_start` runs.
//
// # ⛔ A SYSCALL THIS FILE DOES NOT IMPLEMENT RETURNS ENOSYS, LOUDLY
//
// Every name in `wasi_snapshot_preview1` is bound, so a module never fails to
// instantiate over a missing import — but the ones that are not implemented
// return `ENOSYS` and record themselves in `diagnostics`, which the page
// prints. They must never return 0: a run that silently skipped `path_rename`
// and exited 0 looks exactly like a run that worked, and the person reading the
// output has no way to tell. An error they can see is worth more than an answer
// they cannot check.
//
// # What it deliberately approximates, and says so
//
//   - `poll_oneoff` does not sleep. A clock subscription comes back
//     immediately and records a diagnostic. There is no way to block the
//     browser's main thread for a guest's timer without freezing the page, and
//     a busy-wait would spend the user's CPU to simulate idleness.
//   - `clock_res_get` reports 1µs. Browsers deliberately coarsen their clocks;
//     claiming nanoseconds because the unit is nanoseconds would be a lie.
//   - stdin/stdout/stderr are reported as NOT a terminal by default, which is
//     the shape wasmtime hands a guest whose streams are redirected — the
//     shape the CI runs that proved these modules had. `tty: true` changes it,
//     and changes the output of anything that asks.
//   - The filesystem is in memory and lives exactly as long as the run.
//
// # It is confronted, not asserted
//
// `tools/playground` runs all eight published modules under wasmtime, records
// stdout, stderr and the exit status as fixtures, and then runs the SAME file
// you are reading against the same modules and requires the bytes to match.
// The sqlite3 case in that suite exists because this shim once printed a
// box-drawing table where the reference printed `3.53.4`.

export const ERRNO = Object.freeze({
  SUCCESS: 0,
  ACCES: 2,
  BADF: 8,
  EXIST: 20,
  FAULT: 21,
  INVAL: 28,
  IO: 29,
  ISDIR: 31,
  NAMETOOLONG: 37,
  NOENT: 44,
  NOSYS: 52,
  NOTDIR: 54,
  NOTEMPTY: 55,
  NOTSUP: 58,
  PERM: 63,
  SPIPE: 70,
  NOTCAPABLE: 76,
});

export const FILETYPE = Object.freeze({
  UNKNOWN: 0,
  BLOCK_DEVICE: 1,
  CHARACTER_DEVICE: 2,
  DIRECTORY: 3,
  REGULAR_FILE: 4,
  SOCKET_DGRAM: 5,
  SOCKET_STREAM: 6,
  SYMBOLIC_LINK: 7,
});

const OFLAGS = { CREAT: 1 << 0, DIRECTORY: 1 << 1, EXCL: 1 << 2, TRUNC: 1 << 3 };
const FDFLAGS = { APPEND: 1 << 0, DSYNC: 1 << 1, NONBLOCK: 1 << 2, RSYNC: 1 << 3, SYNC: 1 << 4 };
const WHENCE = { SET: 0, CUR: 1, END: 2 };
const CLOCKID = { REALTIME: 0, MONOTONIC: 1, PROCESS_CPUTIME: 2, THREAD_CPUTIME: 3 };
const EVENTTYPE = { CLOCK: 0, FD_READ: 1, FD_WRITE: 2 };
const PREOPENTYPE_DIR = 0;

// Rights, as bit positions from wasip1.h. BigInt, because `rights` is a u64 and
// the ones a directory needs do not fit in a Number's bit operators.
const R = {
  FD_DATASYNC: 1n << 0n,
  FD_READ: 1n << 1n,
  FD_SEEK: 1n << 2n,
  FD_FDSTAT_SET_FLAGS: 1n << 3n,
  FD_SYNC: 1n << 4n,
  FD_TELL: 1n << 5n,
  FD_WRITE: 1n << 6n,
  FD_ADVISE: 1n << 7n,
  FD_ALLOCATE: 1n << 8n,
  PATH_CREATE_DIRECTORY: 1n << 9n,
  PATH_CREATE_FILE: 1n << 10n,
  PATH_LINK_SOURCE: 1n << 11n,
  PATH_LINK_TARGET: 1n << 12n,
  PATH_OPEN: 1n << 13n,
  FD_READDIR: 1n << 14n,
  PATH_READLINK: 1n << 15n,
  PATH_RENAME_SOURCE: 1n << 16n,
  PATH_RENAME_TARGET: 1n << 17n,
  PATH_FILESTAT_GET: 1n << 18n,
  PATH_FILESTAT_SET_SIZE: 1n << 19n,
  PATH_FILESTAT_SET_TIMES: 1n << 20n,
  FD_FILESTAT_GET: 1n << 21n,
  FD_FILESTAT_SET_SIZE: 1n << 22n,
  FD_FILESTAT_SET_TIMES: 1n << 23n,
  PATH_SYMLINK: 1n << 24n,
  PATH_REMOVE_DIRECTORY: 1n << 25n,
  PATH_UNLINK_FILE: 1n << 26n,
  POLL_FD_READWRITE: 1n << 27n,
};
// `rights` has 30 defined bits, 0 (fd_datasync) through 29 (sock_accept);
// this is wasmtime's `Rights::all()`.
const ALL_RIGHTS = (1n << 30n) - 1n;

// ⛔ THESE SETS ARE COPIED FROM THE REFERENCE, NOT CHOSEN.
//
// wasmtime, crates/wasi/src/p1.rs at v50.0.1, `fd_fdstat_get`:
//
//   - a PREOPENED directory gets the hard-coded list below, commented there as
//     "Hard-coded set or rights expected by many userlands";
//   - any other directory gets `Rights::all()` minus FD_SEEK,
//     FD_FILESTAT_SET_SIZE and PATH_FILESTAT_SET_SIZE;
//   - an open file gets `Rights::all()`, minus FD_READ/FD_READDIR if it is not
//     readable and FD_WRITE if it is not writable.
//
// Guessing here is not cosmetic. The first version of this file handed stdio a
// generous-looking set, and sqlite3 printed a 233-byte box-drawing table where
// wasmtime printed `3.53.4\n`: wasi-libc's isatty() answers from exactly these
// bits, so the rights ARE the behaviour.
const PREOPEN_DIR_RIGHTS =
  R.PATH_CREATE_DIRECTORY | R.PATH_CREATE_FILE | R.PATH_LINK_SOURCE |
  R.PATH_LINK_TARGET | R.PATH_OPEN | R.FD_READDIR | R.PATH_READLINK |
  R.PATH_RENAME_SOURCE | R.PATH_RENAME_TARGET | R.PATH_SYMLINK |
  R.PATH_REMOVE_DIRECTORY | R.PATH_UNLINK_FILE | R.PATH_FILESTAT_GET |
  R.PATH_FILESTAT_SET_TIMES | R.FD_FILESTAT_GET | R.FD_FILESTAT_SET_TIMES;
const PREOPEN_DIR_INHERITING =
  PREOPEN_DIR_RIGHTS | R.FD_DATASYNC | R.FD_READ | R.FD_SEEK |
  R.FD_FDSTAT_SET_FLAGS | R.FD_SYNC | R.FD_TELL | R.FD_WRITE | R.FD_ADVISE |
  R.FD_ALLOCATE | R.FD_FILESTAT_GET | R.FD_FILESTAT_SET_SIZE |
  R.FD_FILESTAT_SET_TIMES | R.POLL_FD_READWRITE;
const OPEN_DIR_RIGHTS = ALL_RIGHTS & ~R.FD_SEEK & ~R.FD_FILESTAT_SET_SIZE & ~R.PATH_FILESTAT_SET_SIZE;
const FILE_RIGHTS = ALL_RIGHTS;

/** Thrown by proc_exit, and by nothing else. `start()` catches it. */
export class WASIExit extends Error {
  constructor(code) {
    super(`proc_exit(${code})`);
    this.name = "WASIExit";
    this.code = code;
  }
}

let nextInode = 1n;

function dirNode(mtim) {
  return { type: FILETYPE.DIRECTORY, ino: nextInode++, entries: new Map(), mtim };
}
function fileNode(data, mtim) {
  return { type: FILETYPE.REGULAR_FILE, ino: nextInode++, data, size: data.length, mtim };
}

/**
 * The whole filesystem a run can see: a tree of directories and byte arrays,
 * built before the module starts and thrown away when it ends. Nothing here
 * touches the host — there is no host to touch.
 */
export class MemFS {
  constructor(now = 0n) {
    this.root = dirNode(now);
    this.now = now;
  }

  /** Create every missing directory along `path`, and return the last one. */
  mkdirp(path) {
    let node = this.root;
    for (const part of splitPath(path)) {
      let next = node.entries.get(part);
      if (next === undefined) {
        next = dirNode(this.now);
        node.entries.set(part, next);
      }
      if (next.type !== FILETYPE.DIRECTORY) throw new Error(`${path}: not a directory`);
      node = next;
    }
    return node;
  }

  /** Seed a file. `data` may be a string or a Uint8Array. */
  writeFile(path, data) {
    const parts = splitPath(path);
    if (parts.length === 0) throw new Error("writeFile: empty path");
    const name = parts.pop();
    const dir = this.mkdirp(parts.join("/"));
    const bytes = typeof data === "string" ? new TextEncoder().encode(data) : new Uint8Array(data);
    dir.entries.set(name, fileNode(bytes, this.now));
  }

  /**
   * Walk `path` from `base`. Returns {parent, name, node}; `node` is undefined
   * when the final component does not exist, and `parent` is undefined when
   * some directory on the way there does not exist (so the caller can tell
   * "the file is missing" from "the directory is missing").
   */
  walk(base, path) {
    const parts = splitPath(path);
    // An absolute path arrives only from a guest that resolved it against a
    // preopen named "/", which is the one this host publishes; treating it as
    // rooted is what makes `--dir=/` behave here as it does under wasmtime.
    let node = path.startsWith("/") ? this.root : base;
    if (parts.length === 0) return { parent: undefined, name: "", node };
    const name = parts.pop();
    for (const part of parts) {
      if (node.type !== FILETYPE.DIRECTORY) return { parent: undefined, name, node: undefined, notdir: true };
      const next = node.entries.get(part);
      if (next === undefined) return { parent: undefined, name, node: undefined };
      node = next;
    }
    if (node.type !== FILETYPE.DIRECTORY) return { parent: undefined, name, node: undefined, notdir: true };
    if (name === "") return { parent: undefined, name: "", node };
    return { parent: node, name, node: node.entries.get(name) };
  }
}

function splitPath(path) {
  const out = [];
  for (const part of String(path).split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      // No escaping upward: a preopen is a capability, and ".." out of it is
      // exactly the thing WASI exists to forbid. Collapsing it is what
      // wasmtime's path resolution does for a sandboxed preopen.
      out.pop();
      continue;
    }
    out.push(part);
  }
  return out;
}

/**
 * A WASI preview1 host.
 *
 * Construct it, hand `imports` to `WebAssembly.instantiate`, then call
 * `start(instance)`. It is deliberately free of browser-only and Node-only
 * APIs: the page and the test harness run the SAME file, so what a test proves
 * is a fact about what the page does.
 */
export class WASI {
  /**
   * @param {object}   opts
   * @param {string[]} opts.args    argv, argv[0] included.
   * @param {object}   opts.env     environment, as a plain object.
   * @param {Uint8Array|string} opts.stdin  the whole of stdin, up front.
   * @param {MemFS}    opts.fs      the filesystem, or a fresh empty one.
   * @param {function} opts.onWrite called with (fd, Uint8Array) on every write
   *                               to fd 1 or 2, for streaming output to a page.
   * @param {function} opts.now     () => milliseconds since the epoch.
   * @param {function} opts.monotonic () => a monotonic millisecond reading.
   * @param {function} opts.randomFill (Uint8Array) => void.
   */
  constructor(opts = {}) {
    this.args = (opts.args ?? []).map(String);
    this.env = opts.env ?? {};
    this.fs = opts.fs ?? new MemFS();
    this.onWrite = opts.onWrite ?? null;
    this._now = opts.now ?? (() => Date.now());
    this._monotonic = opts.monotonic ?? (() => (typeof performance !== "undefined" ? performance.now() : Date.now()));
    this._randomFill = opts.randomFill ?? defaultRandomFill;
    this._started = this._monotonic();

    const stdin = opts.stdin ?? new Uint8Array(0);
    this.stdin = typeof stdin === "string" ? new TextEncoder().encode(stdin) : new Uint8Array(stdin);

    /** Everything written to fd 1 and fd 2, as raw bytes: minigzip's output is
     *  gzip, and decoding it as text would corrupt it before anyone saw it. */
    this.stdout = [];
    this.stderr = [];
    /** One entry per syscall that returned ENOSYS or had to approximate. */
    this.diagnostics = [];

    this.memory = null;
    this._view = null;
    this._bytes = null;
    this.exitCode = null;

    // ⛔ isatty() IS THIS LINE. wasmtime reports a stdio descriptor's filetype
    // as CHARACTER_DEVICE when the host stream is a terminal and UNKNOWN when
    // it is not, with rights of exactly FD_READ or FD_WRITE (p1.rs). wasi-libc
    // calls it a tty when the filetype is CHARACTER_DEVICE *and* neither
    // FD_SEEK nor FD_TELL is granted (libc-bottom-half/sources/isatty.c), so
    // UNKNOWN is what makes sqlite3 print `3.53.4` instead of a box-drawing
    // table. The default is "not a terminal", which is what the CI runs these
    // modules were proven in had; `tty: true` is available and is a DIFFERENT
    // run, not a nicer one.
    const tty = opts.tty === true;
    const stdioType = tty ? FILETYPE.CHARACTER_DEVICE : FILETYPE.UNKNOWN;
    this._fds = new Map();
    this._fds.set(0, { kind: "stdin", pos: 0, type: stdioType, rights: R.FD_READ, inheriting: R.FD_READ, flags: 0 });
    this._fds.set(1, { kind: "stdout", type: stdioType, rights: R.FD_WRITE, inheriting: R.FD_WRITE, flags: 0 });
    this._fds.set(2, { kind: "stderr", type: stdioType, rights: R.FD_WRITE, inheriting: R.FD_WRITE, flags: 0 });
    // Two names for one root. wasi-libc matches an absolute path against the
    // preopen "/", while Go's wasip1 runtime matches its $PWD against whichever
    // preopen is a prefix of it; publishing both is what lets the same tree
    // answer `open("/tmp/x")` and `open("t.md")` from the same run.
    this._fds.set(3, this._preopen("/"));
    this._fds.set(4, this._preopen("."));
    this._nextFd = 5;

    this.imports = { wasi_snapshot_preview1: this._buildImports() };
  }

  _preopen(name) {
    return {
      kind: "dir",
      node: this.fs.root,
      preopen: name,
      type: FILETYPE.DIRECTORY,
      rights: PREOPEN_DIR_RIGHTS,
      inheriting: PREOPEN_DIR_INHERITING,
      flags: 0,
    };
  }

  /** Run the module's `_start`, and report what came of it. */
  start(instance) {
    this.memory = instance.exports.memory;
    if (!this.memory) throw new Error("the module exports no `memory`; application-abi.md requires one");
    if (typeof instance.exports._start !== "function") {
      throw new Error("the module exports no `_start`: it is a reactor, not a command");
    }
    try {
      instance.exports._start();
      this.exitCode = 0;
    } catch (err) {
      if (err instanceof WASIExit) this.exitCode = err.code;
      else throw err;
    }
    return {
      exitCode: this.exitCode,
      stdout: concat(this.stdout),
      stderr: concat(this.stderr),
      diagnostics: this.diagnostics,
    };
  }

  // ---- memory -----------------------------------------------------------
  // Re-derived on every access: `memory.grow` detaches the old ArrayBuffer, and
  // a cached DataView over it throws on the next read. Comparing the buffer
  // identity is cheaper than rebuilding, and correct when it has not moved.

  get _dv() {
    if (this._view === null || this._view.buffer !== this.memory.buffer) {
      this._view = new DataView(this.memory.buffer);
      this._bytes = new Uint8Array(this.memory.buffer);
    }
    return this._view;
  }
  get _u8() {
    this._dv;
    return this._bytes;
  }

  _readString(ptr, len) {
    return new TextDecoder("utf-8").decode(this._u8.subarray(ptr, ptr + len));
  }

  /** Read a ciovec/iovec array: both are {buf: u32, buf_len: u32}, 8 bytes. */
  _iovecs(ptr, count) {
    const dv = this._dv;
    const out = [];
    for (let i = 0; i < count; i++) {
      out.push({ buf: dv.getUint32(ptr + i * 8, true), len: dv.getUint32(ptr + i * 8 + 4, true) });
    }
    return out;
  }

  _note(name, detail) {
    const existing = this.diagnostics.find((d) => d.name === name && d.detail === detail);
    if (existing) existing.count++;
    else this.diagnostics.push({ name, detail, count: 1 });
  }

  /**
   * ⛔ The whole reason this function exists: an import that is bound but NOT
   * implemented. It records itself and returns ENOSYS. It must never return 0.
   */
  _unimplemented(name) {
    return (...args) => {
      this._note(name, `not implemented — returned ENOSYS (${ERRNO.NOSYS}); ${args.length} argument(s)`);
      return ERRNO.NOSYS;
    };
  }

  // ---- clocks -----------------------------------------------------------

  _clockNs(id) {
    switch (id) {
      case CLOCKID.REALTIME:
        return BigInt(Math.round(this._now() * 1e6));
      case CLOCKID.MONOTONIC:
        return BigInt(Math.round(this._monotonic() * 1e6));
      case CLOCKID.PROCESS_CPUTIME:
      case CLOCKID.THREAD_CPUTIME:
        // There is one thread and one process here, and no way to ask the
        // browser how much CPU either has had. Elapsed time since the instance
        // started is the closest honest answer; it is recorded as an
        // approximation rather than passed off as a measurement.
        this._note("clock_time_get", "process/thread CPU time approximated by elapsed monotonic time");
        return BigInt(Math.round((this._monotonic() - this._started) * 1e6));
      default:
        return null;
    }
  }

  // ---- fds --------------------------------------------------------------

  _fd(fd) {
    return this._fds.get(fd);
  }

  _writeTo(entry, bytes) {
    if (entry.kind === "stdout" || entry.kind === "stderr") {
      const copy = bytes.slice();
      (entry.kind === "stdout" ? this.stdout : this.stderr).push(copy);
      if (this.onWrite) this.onWrite(entry.kind === "stdout" ? 1 : 2, copy);
      return bytes.length;
    }
    const node = entry.node;
    const at = entry.flags & FDFLAGS.APPEND ? node.size : entry.pos;
    growFile(node, at + bytes.length);
    node.data.set(bytes, at);
    entry.pos = at + bytes.length;
    node.mtim = BigInt(Math.round(this._now() * 1e6));
    return bytes.length;
  }

  // ---- the import object ------------------------------------------------

  _buildImports() {
    const self = this;
    const impl = {
      args_sizes_get(countPtr, bufSizePtr) {
        const dv = self._dv;
        let size = 0;
        for (const a of self.args) size += new TextEncoder().encode(a).length + 1;
        dv.setUint32(countPtr, self.args.length, true);
        dv.setUint32(bufSizePtr, size, true);
        return ERRNO.SUCCESS;
      },
      args_get(argvPtr, bufPtr) {
        return writeStringVector(self, self.args, argvPtr, bufPtr);
      },
      environ_sizes_get(countPtr, bufSizePtr) {
        const pairs = envPairs(self.env);
        const dv = self._dv;
        let size = 0;
        for (const p of pairs) size += new TextEncoder().encode(p).length + 1;
        dv.setUint32(countPtr, pairs.length, true);
        dv.setUint32(bufSizePtr, size, true);
        return ERRNO.SUCCESS;
      },
      environ_get(environPtr, bufPtr) {
        return writeStringVector(self, envPairs(self.env), environPtr, bufPtr);
      },

      clock_res_get(id, outPtr) {
        if (self._clockNs(id) === null) return ERRNO.INVAL;
        // 1µs. Browsers coarsen their clocks on purpose (Spectre); reporting
        // 1ns because the unit is nanoseconds would be a claim nothing backs.
        self._dv.setBigUint64(outPtr, 1000n, true);
        return ERRNO.SUCCESS;
      },
      clock_time_get(id, _precision, outPtr) {
        const ns = self._clockNs(id);
        if (ns === null) return ERRNO.INVAL;
        self._dv.setBigUint64(outPtr, ns, true);
        return ERRNO.SUCCESS;
      },

      random_get(bufPtr, len) {
        // Web Crypto refuses more than 65 536 bytes in one call, and a shim
        // that ignored that would work for every small request and throw on the
        // first big one.
        const out = self._u8.subarray(bufPtr, bufPtr + len);
        for (let off = 0; off < out.length; off += 65536) {
          self._randomFill(out.subarray(off, Math.min(off + 65536, out.length)));
        }
        return ERRNO.SUCCESS;
      },

      proc_exit(code) {
        throw new WASIExit(code >>> 0);
      },

      sched_yield() {
        // There is one thread, it is this one, and it is already running.
        // Yielding to nobody is the correct no-op, not an unimplemented stub.
        return ERRNO.SUCCESS;
      },

      fd_write(fd, iovsPtr, iovsLen, nwrittenPtr) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        if (entry.kind === "stdin") return ERRNO.BADF;
        if (entry.kind === "dir") return ERRNO.ISDIR;
        let total = 0;
        for (const iov of self._iovecs(iovsPtr, iovsLen)) {
          if (iov.len === 0) continue;
          total += self._writeTo(entry, self._u8.subarray(iov.buf, iov.buf + iov.len));
        }
        self._dv.setUint32(nwrittenPtr, total, true);
        return ERRNO.SUCCESS;
      },

      fd_read(fd, iovsPtr, iovsLen, nreadPtr) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        if (entry.kind === "stdout" || entry.kind === "stderr") return ERRNO.BADF;
        if (entry.kind === "dir") return ERRNO.ISDIR;
        const src = entry.kind === "stdin" ? self.stdin : entry.node.data.subarray(0, entry.node.size);
        let total = 0;
        for (const iov of self._iovecs(iovsPtr, iovsLen)) {
          if (entry.pos >= src.length) break;
          const n = Math.min(iov.len, src.length - entry.pos);
          self._u8.set(src.subarray(entry.pos, entry.pos + n), iov.buf);
          entry.pos += n;
          total += n;
          if (n < iov.len) break;
        }
        self._dv.setUint32(nreadPtr, total, true);
        return ERRNO.SUCCESS;
      },

      fd_pread(fd, iovsPtr, iovsLen, offset, nreadPtr) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        if (entry.kind !== "file") return ERRNO.SPIPE;
        let pos = Number(offset);
        let total = 0;
        const src = entry.node.data.subarray(0, entry.node.size);
        for (const iov of self._iovecs(iovsPtr, iovsLen)) {
          if (pos >= src.length) break;
          const n = Math.min(iov.len, src.length - pos);
          self._u8.set(src.subarray(pos, pos + n), iov.buf);
          pos += n;
          total += n;
          if (n < iov.len) break;
        }
        self._dv.setUint32(nreadPtr, total, true);
        return ERRNO.SUCCESS;
      },

      fd_pwrite(fd, iovsPtr, iovsLen, offset, nwrittenPtr) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        if (entry.kind !== "file") return ERRNO.SPIPE;
        let pos = Number(offset);
        let total = 0;
        for (const iov of self._iovecs(iovsPtr, iovsLen)) {
          if (iov.len === 0) continue;
          growFile(entry.node, pos + iov.len);
          entry.node.data.set(self._u8.subarray(iov.buf, iov.buf + iov.len), pos);
          pos += iov.len;
          total += iov.len;
        }
        self._dv.setUint32(nwrittenPtr, total, true);
        return ERRNO.SUCCESS;
      },

      fd_close(fd) {
        if (!self._fds.has(fd)) return ERRNO.BADF;
        self._fds.delete(fd);
        return ERRNO.SUCCESS;
      },

      fd_seek(fd, offset, whence, outPtr) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        // ESPIPE on a character device is what a pipe does; it is the answer,
        // not a refusal to implement.
        if (entry.kind !== "file") return ERRNO.SPIPE;
        const base =
          whence === WHENCE.SET ? 0 : whence === WHENCE.CUR ? entry.pos : whence === WHENCE.END ? entry.node.size : null;
        if (base === null) return ERRNO.INVAL;
        const pos = base + Number(offset);
        if (pos < 0) return ERRNO.INVAL;
        entry.pos = pos;
        self._dv.setBigUint64(outPtr, BigInt(pos), true);
        return ERRNO.SUCCESS;
      },

      fd_tell(fd, outPtr) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        if (entry.kind !== "file") return ERRNO.SPIPE;
        self._dv.setBigUint64(outPtr, BigInt(entry.pos), true);
        return ERRNO.SUCCESS;
      },

      // fdstat: filetype u8 @0, fdflags u16 @2, rights_base u64 @8,
      // rights_inheriting u64 @16 — size 24 (docs.md, and _Static_assert in
      // wasip1.h).
      fd_fdstat_get(fd, outPtr) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        const dv = self._dv;
        dv.setUint8(outPtr, entry.type);
        dv.setUint16(outPtr + 2, entry.flags, true);
        dv.setBigUint64(outPtr + 8, entry.rights, true);
        dv.setBigUint64(outPtr + 16, entry.inheriting, true);
        return ERRNO.SUCCESS;
      },

      fd_fdstat_set_flags(fd, flags) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        // Everything here is synchronous and never blocks, so NONBLOCK, SYNC,
        // DSYNC and RSYNC are already true of this host; APPEND is honoured by
        // _writeTo. Recording the flags is the whole of the implementation.
        entry.flags = flags & 0xffff;
        return ERRNO.SUCCESS;
      },

      fd_sync(fd) {
        return self._fds.has(fd) ? ERRNO.SUCCESS : ERRNO.BADF;
      },
      fd_datasync(fd) {
        return self._fds.has(fd) ? ERRNO.SUCCESS : ERRNO.BADF;
      },
      fd_advise(fd) {
        // Advisory by definition: a host is allowed to ignore the hint.
        return self._fds.has(fd) ? ERRNO.SUCCESS : ERRNO.BADF;
      },

      fd_allocate(fd, offset, len) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        if (entry.kind !== "file") return ERRNO.BADF;
        growFile(entry.node, Number(offset) + Number(len));
        return ERRNO.SUCCESS;
      },

      fd_filestat_get(fd, outPtr) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        writeFilestat(self, outPtr, entry.node, entry.type);
        return ERRNO.SUCCESS;
      },

      fd_filestat_set_size(fd, size) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        if (entry.kind !== "file") return ERRNO.BADF;
        truncateFile(entry.node, Number(size));
        return ERRNO.SUCCESS;
      },

      // prestat: tag u8 @0, and (align 4) pr_name_len u32 @4 — size 8.
      fd_prestat_get(fd, outPtr) {
        const entry = self._fd(fd);
        if (!entry || entry.preopen === undefined) return ERRNO.BADF;
        const dv = self._dv;
        dv.setUint8(outPtr, PREOPENTYPE_DIR);
        dv.setUint32(outPtr + 4, new TextEncoder().encode(entry.preopen).length, true);
        return ERRNO.SUCCESS;
      },

      fd_prestat_dir_name(fd, pathPtr, pathLen) {
        const entry = self._fd(fd);
        if (!entry || entry.preopen === undefined) return ERRNO.BADF;
        const bytes = new TextEncoder().encode(entry.preopen);
        if (pathLen < bytes.length) return ERRNO.NAMETOOLONG;
        self._u8.set(bytes, pathPtr);
        return ERRNO.SUCCESS;
      },

      fd_renumber(from, to) {
        const entry = self._fd(from);
        if (!entry) return ERRNO.BADF;
        self._fds.set(to, entry);
        self._fds.delete(from);
        return ERRNO.SUCCESS;
      },

      path_open(dirFd, _dirflags, pathPtr, pathLen, oflags, _rightsBase, _rightsInheriting, fdflags, outPtr) {
        const dir = self._fd(dirFd);
        if (!dir) return ERRNO.BADF;
        if (dir.kind !== "dir") return ERRNO.NOTDIR;
        const path = self._readString(pathPtr, pathLen);
        const found = self.fs.walk(dir.node, path);
        if (found.notdir) return ERRNO.NOTDIR;

        let node = found.node;
        if (node === undefined) {
          if (!(oflags & OFLAGS.CREAT)) return ERRNO.NOENT;
          if (found.parent === undefined) return ERRNO.NOENT;
          node = fileNode(new Uint8Array(0), BigInt(Math.round(self._now() * 1e6)));
          found.parent.entries.set(found.name, node);
        } else if (oflags & OFLAGS.EXCL && oflags & OFLAGS.CREAT) {
          return ERRNO.EXIST;
        }
        if (oflags & OFLAGS.DIRECTORY && node.type !== FILETYPE.DIRECTORY) return ERRNO.NOTDIR;
        if (node.type === FILETYPE.REGULAR_FILE && oflags & OFLAGS.TRUNC) truncateFile(node, 0);

        const isDir = node.type === FILETYPE.DIRECTORY;
        const fd = self._nextFd++;
        self._fds.set(fd, {
          kind: isDir ? "dir" : "file",
          node,
          pos: 0,
          type: node.type,
          // Not intersected with the requested rights, because wasmtime does
          // not either: it reports what the descriptor can do. Everything here
          // is readable and writable, so a file gets the full set.
          rights: isDir ? OPEN_DIR_RIGHTS : FILE_RIGHTS,
          inheriting: isDir ? OPEN_DIR_RIGHTS : 0n,
          flags: fdflags & 0xffff,
        });
        self._dv.setUint32(outPtr, fd, true);
        return ERRNO.SUCCESS;
      },

      path_filestat_get(dirFd, _flags, pathPtr, pathLen, outPtr) {
        const dir = self._fd(dirFd);
        if (!dir || dir.kind !== "dir") return ERRNO.BADF;
        const found = self.fs.walk(dir.node, self._readString(pathPtr, pathLen));
        if (found.notdir) return ERRNO.NOTDIR;
        if (found.node === undefined) return ERRNO.NOENT;
        writeFilestat(self, outPtr, found.node, found.node.type);
        return ERRNO.SUCCESS;
      },

      path_create_directory(dirFd, pathPtr, pathLen) {
        const dir = self._fd(dirFd);
        if (!dir || dir.kind !== "dir") return ERRNO.BADF;
        const found = self.fs.walk(dir.node, self._readString(pathPtr, pathLen));
        if (found.notdir) return ERRNO.NOTDIR;
        if (found.node !== undefined) return ERRNO.EXIST;
        if (found.parent === undefined) return ERRNO.NOENT;
        found.parent.entries.set(found.name, dirNode(BigInt(Math.round(self._now() * 1e6))));
        return ERRNO.SUCCESS;
      },

      path_unlink_file(dirFd, pathPtr, pathLen) {
        const dir = self._fd(dirFd);
        if (!dir || dir.kind !== "dir") return ERRNO.BADF;
        const found = self.fs.walk(dir.node, self._readString(pathPtr, pathLen));
        if (found.node === undefined) return ERRNO.NOENT;
        if (found.node.type === FILETYPE.DIRECTORY) return ERRNO.ISDIR;
        found.parent.entries.delete(found.name);
        return ERRNO.SUCCESS;
      },

      path_remove_directory(dirFd, pathPtr, pathLen) {
        const dir = self._fd(dirFd);
        if (!dir || dir.kind !== "dir") return ERRNO.BADF;
        const found = self.fs.walk(dir.node, self._readString(pathPtr, pathLen));
        if (found.node === undefined) return ERRNO.NOENT;
        if (found.node.type !== FILETYPE.DIRECTORY) return ERRNO.NOTDIR;
        if (found.node.entries.size > 0) return ERRNO.NOTEMPTY;
        found.parent.entries.delete(found.name);
        return ERRNO.SUCCESS;
      },

      path_rename(oldFd, oldPtr, oldLen, newFd, newPtr, newLen) {
        const from = self._fd(oldFd);
        const to = self._fd(newFd);
        if (!from || !to || from.kind !== "dir" || to.kind !== "dir") return ERRNO.BADF;
        const src = self.fs.walk(from.node, self._readString(oldPtr, oldLen));
        if (src.node === undefined) return ERRNO.NOENT;
        const dst = self.fs.walk(to.node, self._readString(newPtr, newLen));
        if (dst.parent === undefined) return ERRNO.NOENT;
        dst.parent.entries.set(dst.name, src.node);
        src.parent.entries.delete(src.name);
        return ERRNO.SUCCESS;
      },

      // dirent: d_next u64 @0, d_ino u64 @8, d_namlen u32 @16, d_type u8 @20
      // — size 24 (docs.md `dirent`). The name follows the record.
      fd_readdir(fd, bufPtr, bufLen, cookie, outPtr) {
        const entry = self._fd(fd);
        if (!entry) return ERRNO.BADF;
        if (entry.kind !== "dir") return ERRNO.NOTDIR;
        const names = [".", "..", ...entry.node.entries.keys()];
        const dv = self._dv;
        let written = 0;
        for (let i = Number(cookie); i < names.length; i++) {
          const name = names[i];
          const nameBytes = new TextEncoder().encode(name);
          const node = name === "." || name === ".." ? entry.node : entry.node.entries.get(name);
          if (written + 24 > bufLen) break;
          dv.setBigUint64(bufPtr + written, BigInt(i + 1), true);
          dv.setBigUint64(bufPtr + written + 8, node.ino, true);
          dv.setUint32(bufPtr + written + 16, nameBytes.length, true);
          dv.setUint8(bufPtr + written + 20, node.type);
          written += 24;
          const n = Math.min(nameBytes.length, bufLen - written);
          self._u8.set(nameBytes.subarray(0, n), bufPtr + written);
          written += n;
          if (n < nameBytes.length) break;
        }
        dv.setUint32(outPtr, written, true);
        return ERRNO.SUCCESS;
      },

      // subscription: userdata u64 @0, tag u8 @8, payload @16 — size 48.
      // event: userdata u64 @0, error u16 @8, type u8 @10, nbytes u64 @16 — 32.
      poll_oneoff(inPtr, outPtr, nsubs, neventsPtr) {
        const dv = self._dv;
        let n = 0;
        for (let i = 0; i < nsubs; i++) {
          const sub = inPtr + i * 48;
          const userdata = dv.getBigUint64(sub, true);
          const tag = dv.getUint8(sub + 8);
          const ev = outPtr + n * 32;
          dv.setBigUint64(ev, userdata, true);
          dv.setUint16(ev + 8, ERRNO.SUCCESS, true);
          dv.setUint8(ev + 10, tag);
          dv.setBigUint64(ev + 16, 0n, true);
          dv.setUint16(ev + 24, 0, true);
          if (tag === EVENTTYPE.CLOCK) {
            // ⛔ APPROXIMATION, RECORDED. Blocking here would freeze the page
            // and busy-waiting would spend the reader's CPU pretending to be
            // idle, so the timer is reported as already elapsed. A guest that
            // sleeps therefore sleeps for no time at all — visible in the
            // diagnostics rather than inferred from a wrong duration.
            self._note("poll_oneoff", "a clock subscription returned immediately; this host does not sleep");
            n++;
            continue;
          }
          const fd = dv.getUint32(sub + 16, true);
          const target = self._fd(fd);
          if (!target) {
            dv.setUint16(ev + 8, ERRNO.BADF, true);
            n++;
            continue;
          }
          let nbytes = 0;
          if (tag === EVENTTYPE.FD_READ) {
            nbytes = target.kind === "stdin" ? self.stdin.length - target.pos : (target.node?.size ?? 0) - target.pos;
            if (nbytes < 0) nbytes = 0;
          } else {
            nbytes = 1 << 16; // writes here never block
          }
          dv.setBigUint64(ev + 16, BigInt(nbytes), true);
          n++;
        }
        dv.setUint32(neventsPtr, n, true);
        return ERRNO.SUCCESS;
      },
    };

    // ⛔ Every name preview1 defines is bound, so a module cannot fail to
    // instantiate over a missing import — but the ones above are the only ones
    // IMPLEMENTED. The rest answer ENOSYS and say so.
    const ALL = [
      "args_get", "args_sizes_get", "environ_get", "environ_sizes_get",
      "clock_res_get", "clock_time_get",
      "fd_advise", "fd_allocate", "fd_close", "fd_datasync", "fd_fdstat_get",
      "fd_fdstat_set_flags", "fd_fdstat_set_rights", "fd_filestat_get",
      "fd_filestat_set_size", "fd_filestat_set_times", "fd_pread",
      "fd_prestat_get", "fd_prestat_dir_name", "fd_pwrite", "fd_read",
      "fd_readdir", "fd_renumber", "fd_seek", "fd_sync", "fd_tell", "fd_write",
      "path_create_directory", "path_filestat_get", "path_filestat_set_times",
      "path_link", "path_open", "path_readlink", "path_remove_directory",
      "path_rename", "path_symlink", "path_unlink_file",
      "poll_oneoff", "proc_exit", "proc_raise", "sched_yield", "random_get",
      "sock_accept", "sock_recv", "sock_send", "sock_shutdown",
    ];
    const out = {};
    for (const name of ALL) out[name] = impl[name] ?? this._unimplemented(name);
    return out;
  }
}

function envPairs(env) {
  return Object.entries(env).map(([k, v]) => `${k}=${v}`);
}

function writeStringVector(self, items, ptrArray, bufPtr) {
  const dv = self._dv;
  const u8 = self._u8;
  const enc = new TextEncoder();
  let off = bufPtr;
  for (let i = 0; i < items.length; i++) {
    dv.setUint32(ptrArray + i * 4, off, true);
    const bytes = enc.encode(items[i]);
    u8.set(bytes, off);
    off += bytes.length;
    u8[off++] = 0;
  }
  return ERRNO.SUCCESS;
}

// filestat: dev u64 @0, ino u64 @8, filetype u8 @16, nlink u64 @24,
// size u64 @32, atim @40, mtim @48, ctim @56 — size 64 (docs.md `filestat`).
function writeFilestat(self, ptr, node, filetype) {
  const dv = self._dv;
  dv.setBigUint64(ptr, 0n, true);
  dv.setBigUint64(ptr + 8, node ? node.ino : 0n, true);
  dv.setUint8(ptr + 16, filetype);
  dv.setBigUint64(ptr + 24, 1n, true);
  dv.setBigUint64(ptr + 32, BigInt(node && node.size !== undefined ? node.size : 0), true);
  const t = node ? node.mtim : 0n;
  dv.setBigUint64(ptr + 40, t, true);
  dv.setBigUint64(ptr + 48, t, true);
  dv.setBigUint64(ptr + 56, t, true);
}

function growFile(node, size) {
  if (size <= node.data.length) {
    if (size > node.size) node.size = size;
    return;
  }
  // Double, so writing a file one small write at a time is not quadratic.
  const cap = Math.max(size, node.data.length * 2, 1024);
  const next = new Uint8Array(cap);
  next.set(node.data.subarray(0, node.size));
  node.data = next;
  node.size = size;
}

function truncateFile(node, size) {
  if (size > node.data.length) growFile(node, size);
  else if (size > node.size) node.data.fill(0, node.size, size);
  node.size = size;
}

function concat(chunks) {
  let total = 0;
  for (const c of chunks) total += c.length;
  const out = new Uint8Array(total);
  let off = 0;
  for (const c of chunks) {
    out.set(c, off);
    off += c.length;
  }
  return out;
}

function defaultRandomFill(buf) {
  if (typeof crypto !== "undefined" && crypto.getRandomValues) {
    crypto.getRandomValues(buf);
    return;
  }
  throw new Error("no crypto.getRandomValues: this host cannot answer random_get");
}
