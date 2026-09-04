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
