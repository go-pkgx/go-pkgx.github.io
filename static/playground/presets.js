// The one invocation per module that is KNOWN to work.
//
// These are not examples somebody thought up: go-pkgx/packages runs exactly
// these arguments, with exactly these files and this stdin, under wasmtime
// before it publishes the bottle — `.github/workflows/wasip1.yml` for the Go
// lane and `wasip1-c.yml` for the C and C++ one. A default the page invented
// would be a guess about somebody else's command line, and the first person to
// press Run would be the one who found out.
//
// ⛔ THEY ARE IN THEIR OWN FILE SO A TEST CAN READ THEM. `tools/playground`
// imports this module and requires every entry to match the fixture it
// confronts wasmtime with, so editing a preset here and nowhere else fails the
// build instead of quietly putting an unproven command in front of a reader.

export const PRESETS = {
  age: { args: ["--version"] },
  "go-md2man": { args: ["-in", "t.md"], files: { "t.md": "# Title\n\nbody\n" } },
  shfmt: { args: ["--version"] },
  lua: { args: ["-e", "assert(not pcall(error, 1)); assert(_VERSION == [[Lua 5.4]])"] },
  luac: { args: ["-v"] },
  sqlite3: { args: [":memory:", "select sqlite_version();"] },
  minigzip: { args: [], stdin: "hello wasip1\n", files: { "t.txt": "hello wasip1\n" } },
  "simdjson-minify": { args: [], stdin: '{ "a" : [1, 2 ] }\n', files: { "j.json": '{ "a" : [1, 2 ] }\n' } },
};
