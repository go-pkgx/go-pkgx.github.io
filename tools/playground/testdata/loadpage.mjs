// Loads every script the playground page loads, in a runtime with no DOM, and
// prints what it would put in front of a reader.
//
// Three things fall out of it. Importing app.mjs PARSES AND EVALUATES the
// page's controller, so a syntax error or a bad import specifier fails a Go
// test here instead of showing up as a blank page in somebody's browser.
// Printing PRESETS lets that test require the page's defaults to be the very
// invocations the fixtures were recorded from. And `roundtrip` runs each
// preset through the page's OWN quote-then-tokenise path — which is what
// actually reaches argv when somebody presses Run, and is therefore the thing
// that has to be right, rather than the array the preset started as.

import { tokenise, quote } from "./app.mjs";
import { PRESETS } from "./presets.mjs";

const out = {};
for (const [bin, preset] of Object.entries(PRESETS)) {
  out[bin] = {
    ...preset,
    roundtrip: tokenise([bin, ...preset.args].map(quote).join(" ")),
  };
}
process.stdout.write(JSON.stringify(out) + "\n");
