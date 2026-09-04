if (typeof globalThis.structuredClone !== "function") {
  globalThis.structuredClone = (value) => JSON.parse(JSON.stringify(value));
}

import daisyui from "daisyui";
import typography from "@tailwindcss/typography";
import tailwindPkg from "tailwindcss/package.json";
import tailwindIndex from "tailwindcss/index.css";

const modules = {
  daisyui: { default: daisyui },
  "@tailwindcss/typography": { default: typography },
};

globalThis.__tw_files = {
  "tailwindcss/package.json": JSON.stringify(tailwindPkg),
  "tailwindcss/index.css": tailwindIndex,
  tailwindcss: tailwindIndex,
  "tailwindcss/index": tailwindIndex,
};

globalThis.__tw_import = (id) => {
  const loaded = globalThis.__tw_load?.(String(id));
  if (loaded) {
    return Promise.resolve(loaded);
  }
  return Promise.reject(new Error("dynamic import: " + id));
};

globalThis.__tw_resolve = (id) => {
  if (id in modules || id in globalThis.__tw_files) {
    return "/virtual/" + id;
  }
  return undefined;
};

globalThis.__tw_load = async (url) => {
  const id = String(url)
    .replace(/^file:\/\//, "")
    .replace(/^\/virtual\//, "")
    .replace(/\?.*$/, "");
  if (id in modules) {
    return modules[id];
  }
  return undefined;
};
