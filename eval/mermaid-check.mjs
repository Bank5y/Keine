// Parses Mermaid diagrams with mermaid's own parser, for keine-eval (#140).
// Reads a JSON array of diagram sources on stdin and writes a JSON array
// of the same length: null for a diagram that parses, the parser's
// message for one that does not.
//
//   node eval/mermaid-check.mjs <web dir with node_modules> < diagrams.json
//
// mermaid sanitizes labels with DOMPurify, which has nothing to work on
// outside a browser; parsing never needs it, so it gets no-op hooks.
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import path from "node:path";

const web = path.resolve(process.argv[2] ?? "web");
const at = (p) => pathToFileURL(path.join(web, "node_modules", p)).href;
const { default: purify } = await import(at("dompurify/dist/purify.es.mjs"));
if (typeof purify.addHook !== "function") {
  purify.addHook = () => {};
  purify.sanitize = (s) => s;
}
const { default: mermaid } = await import(at("mermaid/dist/mermaid.core.mjs"));

const out = [];
for (const src of JSON.parse(readFileSync(0, "utf8"))) {
  try {
    await mermaid.parse(src);
    out.push(null);
  } catch (e) {
    out.push(String(e?.message ?? e));
  }
}
process.stdout.write(JSON.stringify(out));
