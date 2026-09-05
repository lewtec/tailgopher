# tailgopher Specification

This document constrains the `tailwind` command: a self-contained binary that compiles Tailwind CSS v4. The operator has no JS toolchain.

Status: draft
Genre: cli

The key words MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY in this
document are to be interpreted as described in BCP 14 (RFC 2119,
RFC 8174) when, and only when, they appear in all capitals.

## Intention

Job: One self-contained `tailwind` binary compiles Tailwind-classed sources into one CSS stylesheet. A closed plugin catalog (daisyUI, Typography) is inside the binary. The operator has no Node, Bun, Deno, or other JS toolchain.

Non-goals:

1. A JS package manager for the operator.
2. A general CSS preprocessor (Sass, Less).
3. A component library owned by this repo.
4. A plugin-authoring SDK.
5. A Vite replacement, browser preview, or live-reload server.
6. An importable Go `Compile` API (later work).
7. A Tailwind v3 JS-config engine.
8. Resolving plugins from the operator’s `node_modules`. The catalog is the one baked into the binary.

Inherited C (cite the file): `mise.toml` pins Go and Node for maintainers of this repo. `package.json` is the generate-time npm manifest. The operator tree has neither requirement.

## Technique

| ID | Input | Rule | Output |
|----|-------|------|--------|
| TEC-01 | Operator argv | Run official `@tailwindcss/cli` inside the embedded JS VM. This command does not parse flags or compile CSS itself | Official CLI process (files, stdout, exit) |
| TEC-02 | Scan request from the official CLI | Official Oxide, hosted as WASM in the embedded WASM VM | Candidate list the CLI already expects |
| TEC-03 | `--minify` / `--optimize` as the official CLI implements them | Official Lightning CSS, hosted as WASM | Optimized stylesheet |
| TEC-04 | `--watch` / `--poll` as the official CLI implements them | Official CLI watcher. Prefer `--poll` when `@parcel/watcher` cannot load | Rebuilt output CSS |
| TEC-05 | `@plugin` name the official CLI asks to resolve | Resolve `daisyui`, `daisyui/theme`, and `@tailwindcss/typography` from the generated bundle via the CLI’s `__tw_resolve` / `__tw_load` hooks. Unknown names fail as the CLI fails them | Plugin module |
| TEC-06 | This repo’s `package.json` dependencies | `go generate` runs esbuild and writes the official CLI bundle plus WASM modules into the module | Embeddable engine artifacts |

## Tooling

| TEC | Tool | Relation | We do not | Cite |
|-----|------|----------|-----------|------|
| TEC-01 | official `@tailwindcss/cli`, executed in goja | wrap | write a second flag dialect, scanner, or compile API | npm:@tailwindcss/cli |
| TEC-02 | `@tailwindcss/oxide-wasm32-wasi` in wazero | wrap | write a class scanner | npm:@tailwindcss/oxide-wasm32-wasi |
| TEC-03 | `lightningcss-wasm` hosted from the JS VM | wrap | write a CSS minifier | npm:lightningcss-wasm |
| TEC-04 | official CLI `--watch` / `--poll` | adopt | a second watch loop | npm:@tailwindcss/cli |
| TEC-05 | catalog copies of daisyUI and `@tailwindcss/typography` inside the generate bundle | adopt | load arbitrary operator JS | npm:daisyui, npm:@tailwindcss/typography |
| TEC-06 | esbuild via this repo’s `package.json` + Node (maintainers only) | adopt | ask the operator to run npm | npm:esbuild, path:package.json |

| Cell | Pick | C or D | Implements | Cite if C |
|------|------|--------|------------|-----------|
| Language | Go | C | TEC-01, `cmd/tailwind` | mise.toml |
| Runtime | goja + wazero. Operator process MUST NOT exec `node` or `bun` | D | TEC-01–TEC-03 | |
| Persistence | none | D | | |
| UI | none | D | | |
| Packaging | One executable from `cmd/tailwind`. The generate-time bundle and WASM are embedded in that binary. `go install` / `go tool` / a release build are how the operator obtains it | D | TEC-06 | |
| Identity | none | D | | |
| Host OS | any OS the Go toolchain supports | D | | |

## Terminology

| Concept | Approved | Banned |
|---------|----------|--------|
| Person who runs the command | operator | user, developer, client |
| Person who changes this repo | maintainer | contributor (in law) |
| Official Tailwind v4 compile step | compile | transpile, process, build (except `compiler.build`) |
| Class names fed to `compiler.build` | candidates | classes, utilities (as the list) |
| Closed set of `@plugin` names | catalog | plugin registry, marketplace |
| JS file produced by `go generate` | bundle | pack, chunk |
| Embedded JS engine | goja | Node, Bun, Deno, V8, JS runtime (those names mean an operator-installed toolchain) |
| Embedded WASM engine | wazero | wasmtime, WasmEdge (when naming the engine) |
| Stylesheet this command writes | output CSS | artifact, dist |

## Types

| Type | Exported | Identity or value | Mutable | Nil/error | Callers MUST NOT |
|------|----------|-------------------|---------|-----------|------------------|
| CompileRequest | command flags and input CSS | value | no after parse | invalid flags fail parse | invent a second request shape |
| Stylesheet | output CSS bytes | value | no | empty is valid | treat as a file tree |
| CandidateSet | list of class tokens | value | replaced per compile | empty is valid | invent utilities the compiler did not see |
| PluginName | `daisyui`, `daisyui/theme`, or `@tailwindcss/typography` | value | no | unknown name is an error | pass a filesystem path as a catalog name |
| EngineBundle | generated JS plus WASM embedded in the binary | identity = files from TEC-06 | maintainer generate only | missing bundle is a maintainer error | fetch npm at operator runtime |

| Command | Type it mutates | Transition | Bad input |
|---------|-----------------|------------|-----------|
| `tailwind` | Stylesheet at `--output` | CompileRequest → write output CSS | see Errors |

## Invariants

| ID | Predicate | On | Forbidden bypass |
|----|-----------|----|------------------|
| INV-01 | The operator process does not require `node`, `bun`, or `deno` on PATH and does not invoke them | `tailwind` | “fallback to npx if present” |
| INV-02 | The only `@plugin` names that resolve are `daisyui`, `daisyui/theme`, and `@tailwindcss/typography` | TEC-05 | loading an operator-supplied `.mjs` or `node_modules` plugin as if it were catalog |
| INV-03 | The command writes only the `--output` path (file or stdout) | Stylesheet | rewriting input CSS, writing `tailwind.config.js`, creating `node_modules` |
| INV-04 | The shipped binary embeds the engine bundle. Running it does not run npm and does not read a sibling `node_modules` | EngineBundle | a multi-file runtime next to the binary |
| INV-05 | The command is official `@tailwindcss/cli` hosted in-process, not a second Tailwind dialect | TEC-01 | a hand-written flag parser, scanner, or utility table |
| INV-06 | The operator-facing artifact is one executable | Packaging | requiring a JS toolchain to compile CSS |

## Errors

| Public operation | Bad input | One reaction |
|------------------|-----------|--------------|
| official CLI argv | unknown flag or missing input | official CLI stderr and exit code |
| resolve `@plugin` | name outside the catalog | official CLI resolve error; non-zero exit |
| scan / compile / optimize / write | official engine or I/O failure | official CLI stderr and exit code |

## Actors

| Actor | Obligations |
|-------|-------------|
| operator | Runs the `tailwind` binary. Supplies input CSS and sources. MUST NOT be required to install a JS toolchain |
| maintainer | Runs `go generate` in this repo. MAY use Node and npm here |

## Capabilities

| ID | Actor | Sea-level goal |
|----|-------|----------------|
| CAP-01 | operator | Compile input CSS and sources to output CSS |
| CAP-02 | operator | Enable catalog plugins with `@plugin` in input CSS |
| CAP-03 | operator | Rebuild output CSS when sources change (`--watch`) |
| CAP-04 | maintainer | Regenerate the engine bundle from `package.json` |

## Quality

| Concern | Measure, or why it cannot happen |
|---------|----------------------------------|
| exit contract | Exit 0 on success. Exit 2 on usage or unreadable input or unknown plugin. Exit 1 on compile, scan, optimize, write, or fatal watch failure |
| untrusted input | Input CSS, `@source` paths, and content files are untrusted. The command MUST NOT exec `node` or `bun`. The command MUST NOT evaluate JS outside the generated bundle and catalog |

## Security

In scope: untrusted input CSS and source trees on the operator machine. The command reads those files and writes one output path.

Why it cannot happen: this command is not a network service and has no identity store.

Residual risk: bugs in goja or wazero while evaluating the official bundle. Catalog plugins run inside goja.

## Success

- [ ] `go run ./cmd/tailwind -i testdata/input.css -o testdata/out.css` writes CSS that contains a utility present in `testdata/index.html` and does not start `node` or `bun`.
- [ ] The same command with `@plugin "daisyui"` in the input CSS emits a daisyUI rule for a daisyUI class used in the HTML.
- [ ] The same command with `@plugin "daisyui/theme"` emits the named theme’s tokens.
- [ ] The same command with `@plugin "@tailwindcss/typography"` emits a `prose` rule when that class is used.
- [ ] `@plugin "not-in-catalog"` exits 2 and writes no output CSS file.
- [ ] A machine without Node, Bun, or Deno can compile CSS with the shipped `tailwind` binary.

## Later work

1. Importable Go `Compile` API (library surface).
2. More first-party Tailwind plugins in the catalog.
3. Tailwind v3 JS-config pipeline.

## Assumptions

| ID | Fact | If false |
|----|------|----------|
| AS-01 | Official `tailwindcss` `compile` / `build`, after esbuild, runs in goja | Change the JS host. Do not reimplement the design system |
| AS-02 | Official Oxide WASM runs in wazero | Keep official Oxide. Change the WASM host |
| AS-03 | Official Lightning CSS WASM is callable from the JS host | Keep official Lightning. Change how the WASM is instantiated |

## Decision history

- ADR-0001: Host official `@tailwindcss/cli` in goja, with Oxide and Lightning as WASM. Rejected: a second flag parser or compile API. Rejected: wrapping the standalone native binary as the engine. Rejected: a Go rewrite of the design system. Rejected: requiring Node on the operator PATH.
- ADR-0002: Closed catalog `daisyui` (including `daisyui/theme`) and `@tailwindcss/typography`. Rejected: open loading of operator JS plugins.
- ADR-0003: Maintainers generate the bundle with Node, npm, and esbuild in this repo. The operator binary embeds that bundle. Rejected: a Node-free generate step as a v1 gate.
- ADR-0004: Ship one self-contained executable. Catalog plugins are baked in. Rejected: requiring a JS runtime on the operator machine. Rejected: resolving `@plugin` from the operator’s `node_modules`.
