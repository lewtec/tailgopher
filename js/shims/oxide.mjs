import fs from "fs";
import path from "path";

const skipDir = new Set(["node_modules", ".git", "dist", "vendor"]);

class ShimScanner {
  constructor(opts = {}) {
    this.sources = opts.sources || [];
    this._files = [];
    this._scanned = [];
  }

  scan() {
    const candidates = new Set();
    this._files = this.#collect();
    this._scanned = this._files.slice();
    for (const file of this._files) {
      this.#extract(readText(file), candidates);
    }
    return Array.from(candidates);
  }

  scanFiles(input) {
    const candidates = new Set();
    for (const item of input || []) {
      const content =
        item.content != null
          ? item.content
          : item.file
            ? readText(item.file)
            : "";
      if (item.file) {
        this._scanned.push(item.file);
      }
      this.#extract(content, candidates);
    }
    return Array.from(candidates);
  }

  getCandidatesWithPositions(input) {
    const content =
      input.content != null
        ? input.content
        : input.file
          ? readText(input.file)
          : "";
    return extractPositions(content);
  }

  get files() {
    return this._files;
  }

  get scannedFiles() {
    return this._scanned;
  }

  get globs() {
    return this.normalizedSources;
  }

  get normalizedSources() {
    return this.sources.map((s) => ({ base: s.base, pattern: s.pattern }));
  }

  #collect() {
    const files = new Set();
    const excluded = new Set();
    for (const src of this.sources) {
      const dest = src.negated ? excluded : files;
      collectGlob(src.base, src.pattern, dest);
    }
    for (const f of excluded) {
      files.delete(f);
    }
    return Array.from(files);
  }

  #extract(content, candidates) {
    for (const tok of extractTokens(content)) {
      candidates.add(tok);
    }
  }
}

export const Scanner = wrapScanner(
  typeof globalThis.__tw_oxide_scanner === "function"
    ? globalThis.__tw_oxide_scanner
    : null,
  ShimScanner,
);

function wrapScanner(Official, Shim) {
  if (!Official) {
    return Shim;
  }
  return class Scanner {
    constructor(opts = {}) {
      this._opts = opts;
      this._shim = null;
      try {
        this._native = new Official(opts);
      } catch {
        this._native = null;
        this._shim = new Shim(opts);
      }
    }
    scan() {
      return this.#call("scan", []);
    }
    scanFiles(input) {
      return this.#call("scanFiles", [input]);
    }
    getCandidatesWithPositions(input) {
      return this.#call("getCandidatesWithPositions", [input]);
    }
    get files() {
      return this.#get("files");
    }
    get scannedFiles() {
      return this.#get("scannedFiles");
    }
    get globs() {
      return this.#get("globs");
    }
    get normalizedSources() {
      return this.#get("normalizedSources");
    }
    #impl() {
      return this._shim || this._native;
    }
    #fallback() {
      if (!this._shim) {
        this._shim = new Shim(this._opts);
      }
      return this._shim;
    }
    #call(name, args) {
      const cur = this.#impl();
      try {
        return cur[name](...args);
      } catch {
        return this.#fallback()[name](...args);
      }
    }
    #get(name) {
      const cur = this.#impl();
      try {
        return cur[name];
      } catch {
        return this.#fallback()[name];
      }
    }
  };
}

function readText(file) {
  return fs.readFileSync(file, "utf8");
}

function collectGlob(base, pattern, out) {
  if (!base || !pattern) {
    return;
  }
  if (!/[*?]/.test(pattern)) {
    const abs = path.resolve(base, pattern);
    if (isFile(abs)) {
      out.add(abs);
    }
    return;
  }
  const re = globRegExp(pattern);
  const root = globRoot(base, pattern);
  walk(root, (file) => {
    const rel = relPosix(base, file);
    if (re.test(rel) || re.test(relPosix(root, file))) {
      out.add(file);
    }
  });
}

function globRoot(base, pattern) {
  const parts = pattern.split(/[\\/]/);
  const prefix = [];
  for (const part of parts) {
    if (part.includes("*") || part.includes("?")) {
      break;
    }
    prefix.push(part);
  }
  return path.resolve(base, ...prefix);
}

function globRegExp(pattern) {
  let body = "";
  for (let i = 0; i < pattern.length; i++) {
    const c = pattern[i];
    if (c === "*" && pattern[i + 1] === "*") {
      const next = pattern[i + 2];
      if (next === "/" || next === "\\") {
        body += "(?:.*/)?";
        i += 2;
        continue;
      }
      body += ".*";
      i += 1;
      continue;
    }
    if (c === "*") {
      body += "[^/]*";
      continue;
    }
    if (c === "?") {
      body += "[^/]";
      continue;
    }
    if ("\\^$+{}[]()|.".includes(c)) {
      body += "\\" + c;
      continue;
    }
    if (c === "\\") {
      body += "[\\\\/]";
      continue;
    }
    body += c === "/" ? "[\\\\/]" : c;
  }
  return new RegExp("^" + body + "$");
}

function walk(dir, fn) {
  let entries;
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const ent of entries) {
    const p = path.join(dir, ent.name);
    if (ent.isDirectory()) {
      if (skipDir.has(ent.name)) {
        continue;
      }
      walk(p, fn);
      continue;
    }
    if (ent.isFile()) {
      fn(p);
    }
  }
}

function isFile(p) {
  try {
    return fs.statSync(p).isFile();
  } catch {
    return false;
  }
}

function relPosix(from, to) {
  return path.relative(from, to).split(path.sep).join("/");
}

const classAttr =
  /(?:class|className)\s*=\s*(?:"([^"]*)"|'([^']*)'|`([^`]*)`)/g;

function extractTokens(content) {
  const out = new Set();
  classAttr.lastIndex = 0;
  let m;
  while ((m = classAttr.exec(content))) {
    addClassList(m[1] || m[2] || m[3] || "", out);
  }
  const tokenRe = /!?[a-zA-Z][\w@<>\[\]!/%#.:-]*/g;
  while ((m = tokenRe.exec(content))) {
    const tok = m[0];
    if (looksLikeCandidate(tok)) {
      out.add(tok);
    }
  }
  return out;
}

function extractPositions(content) {
  const out = [];
  const tokenRe = /!?[a-zA-Z][\w@<>\[\]!/%#.:-]*/g;
  let m;
  while ((m = tokenRe.exec(content))) {
    if (looksLikeCandidate(m[0])) {
      out.push({ candidate: m[0], position: m.index });
    }
  }
  return out;
}

function addClassList(s, out) {
  for (const part of s.split(/\s+/)) {
    if (part) {
      out.add(part);
    }
  }
}

function looksLikeCandidate(tok) {
  if (tok.length < 2) {
    return false;
  }
  if (tok.includes("-") || tok.includes(":") || tok.includes("/") || tok.includes("[")) {
    return true;
  }
  return /^(flex|grid|block|hidden|inline|relative|absolute|fixed|sticky|contents|btn|card|link|menu|navbar|drawer|modal|table|badge|tab|swap|prose|input|textarea|select|checkbox|toggle|alert|toast|footer|hero|divider|stack|join|mask|kbd|stat|steps|timeline|loading|skeleton|avatar|chat|collapse|dropdown|tooltip|indicator|countdown|radial|range|rating|file|label|fieldset)$/.test(
    tok,
  );
}
