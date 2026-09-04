import { execFile } from "node:child_process";
import { promisify } from "node:util";
import * as esbuild from "esbuild";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const execFileP = promisify(execFile);

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), "..");

const result = await esbuild.build({
  absWorkingDir: root,
  entryPoints: ["js/entry.mjs"],
  bundle: true,
  format: "iife",
  platform: "node",
  target: "es2017",
  outfile: "internal/engine/bundle.js",
  loader: { ".css": "text", ".json": "json" },
  alias: {
    "util-deprecate": path.join(root, "js/shims/deprecate.cjs"),
    "@tailwindcss/oxide": path.join(root, "js/shims/oxide.mjs"),
    lightningcss: path.join(root, "js/shims/lightningcss.mjs"),
    jiti: path.join(root, "js/shims/jiti.mjs"),
  },
  plugins: [
    {
      name: "cli-no-tla",
      setup(build) {
        build.onLoad(
          { filter: /@tailwindcss[/\\]cli[/\\]dist[/\\]index\.mjs$/ },
          async (args) => {
            let text = await fs.readFile(args.path, "utf8");
            text = text.replace(
              /await Ot\(\);?\s*$/,
              "globalThis.__tw_done = Ot();",
            );
            return {
              contents: text,
              loader: "js",
              resolveDir: path.dirname(args.path),
            };
          },
        );
      },
    },
  ],
  logLevel: "info",
});

if (result.errors.length > 0) {
  process.exit(1);
}

const wasmDir = path.join(root, "internal/engine/wasm");
await fs.mkdir(wasmDir, { recursive: true });
await fs.copyFile(
  path.join(root, "node_modules/lightningcss-wasm/lightningcss_node.wasm"),
  path.join(wasmDir, "lightningcss.wasm"),
);
await copyOxideWasm(wasmDir);

const out = path.join(root, "internal/engine/bundle.js");
let js = await fs.readFile(out, "utf8");
js = js.replace(/\bimport\(([^)]+)\)/g, "globalThis.__tw_import($1)");
js = js.replace(
  /var \{ fileURLToPath \} = __require\("url"\);/g,
  `var fileURLToPath = function (u) {
    if (u && typeof u === "object") {
      if (typeof u.pathname === "string" && u.pathname) return u.pathname;
      if (typeof u.href === "string") return String(u.href).replace(/^file:\\/\\//, "");
    }
    return String(u).replace(/^file:\\/\\//, "");
  };`,
);
js = js.replace(
  /var toPath = \(maybeURL\) => maybeURL instanceof URL \? fileURLToPath\(maybeURL\) : maybeURL;/g,
  "var toPath = function (maybeURL) { try { return fileURLToPath(maybeURL); } catch (e) { return maybeURL; } };",
);
await fs.writeFile(out, js);

async function copyOxideWasm(wasmDir) {
  const dest = path.join(wasmDir, "oxide.wasm");
  const local = path.join(
    root,
    "node_modules/@tailwindcss/oxide-wasm32-wasi/tailwindcss-oxide.wasm32-wasi.wasm",
  );
  try {
    await fs.copyFile(local, dest);
    return;
  } catch {
    // npm refuses cpu wasm32 on the host; pull the tarball
  }
  const { version } = JSON.parse(
    await fs.readFile(path.join(root, "node_modules/tailwindcss/package.json"), "utf8"),
  );
  const url = `https://registry.npmjs.org/@tailwindcss/oxide-wasm32-wasi/-/oxide-wasm32-wasi-${version}.tgz`;
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`download oxide wasm: ${res.status} ${url}`);
  }
  const tgz = path.join(wasmDir, ".oxide.tgz");
  const unpack = path.join(wasmDir, ".oxide-pkg");
  await fs.writeFile(tgz, Buffer.from(await res.arrayBuffer()));
  await fs.mkdir(unpack, { recursive: true });
  try {
    await execFileP("tar", ["-xzf", tgz, "-C", unpack, "--strip-components=1"]);
    await fs.copyFile(
      path.join(unpack, "tailwindcss-oxide.wasm32-wasi.wasm"),
      dest,
    );
  } finally {
    await fs.rm(tgz, { force: true });
    await fs.rm(unpack, { recursive: true, force: true });
  }
}
