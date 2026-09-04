export const Features = {
  Nesting: 1,
  NotSelectorList: 2,
  DirSelector: 4,
  LangSelectorList: 8,
  IsSelector: 16,
  TextDecorationThicknessPercent: 32,
  MediaIntervalSyntax: 64,
  MediaRangeSyntax: 128,
  CustomMediaQueries: 256,
  ClampFunction: 512,
  ColorFunction: 1024,
  OklabColors: 2048,
  LabColors: 4096,
  P3Colors: 8192,
  HexAlphaColors: 16384,
  SpaceSeparatedColorNotation: 32768,
  FontFamilySystemUi: 65536,
  DoublePositionGradients: 131072,
  VendorPrefixes: 262144,
  LogicalProperties: 524288,
  LightDark: 1048576,
  Selectors: 31,
  MediaQueries: 448,
  Colors: 1113088,
};

export function transform(opts = {}) {
  if (typeof globalThis.__tw_lightning === "function") {
    return normalizeLightning(globalThis.__tw_lightning(opts));
  }
  const input = opts.code;
  let css = typeof input === "string" ? input : bytesToString(input);
  if (opts.minify) {
    css = minifyCSS(css);
  }
  return {
    code: {
      toString() {
        return css;
      },
    },
    map: undefined,
    warnings: [],
  };
}

function normalizeLightning(r) {
  if (!r || typeof r !== "object") {
    return r;
  }
  const src = r.warnings;
  const out = [];
  const n = src && src.length != null ? Number(src.length) : 0;
  for (let i = 0; i < n; i++) {
    const w = src[i];
    if (!w || typeof w !== "object") {
      continue;
    }
    if (w.loc == null || typeof w.loc.line !== "number") {
      w.loc = { line: 1, column: 1 };
    }
    out.push(w);
  }
  r.warnings = out;
  return r;
}

function bytesToString(code) {
  if (code == null) {
    return "";
  }
  if (typeof code === "string") {
    return code;
  }
  if (typeof code.toString === "function" && code.toString !== Array.prototype.toString) {
    const s = code.toString("utf8");
    if (typeof s === "string" && s !== "[object Object]") {
      return s;
    }
  }
  if (code.length != null) {
    let out = "";
    for (let i = 0; i < code.length; i++) {
      out += String.fromCharCode(code[i]);
    }
    return out;
  }
  return String(code);
}

function minifyCSS(css) {
  return css
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/\s+/g, " ")
    .replace(/\s*([{}:;,])\s*/g, "$1")
    .trim();
}
