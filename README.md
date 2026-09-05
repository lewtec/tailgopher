# tailgopher

<p align="center">
  <img src="docs/assets/gopher.png" width="280" alt="tailgopher: a teal Go gopher with a Tailwind-style wave of hair">
</p>

tailgopher is a `tailwind` command for Go projects. It compiles Tailwind CSS v4.
You do not install Node, npm, Bun, or Deno to run it.

The command is official [`@tailwindcss/cli`](https://www.npmjs.com/package/@tailwindcss/cli)
inside the Go process. The JS runs in [goja](https://github.com/dop251/goja).
Oxide and Lightning CSS run as official WASM in [wazero](https://github.com/tetratelabs/wazero).

This repo is not a Tailwind Labs product. See [Relation to Tailwind CSS](#relation-to-tailwind-css).

## Use as a Go tool

Go 1.24 and later can record a developer command in `go.mod` with a `tool` directive.
See [Tool dependencies](https://go.dev/doc/modules/managing-dependencies#tools) on go.dev.

This module sets `go 1.27.0`. `go get -tool` raises your module `go` line to
at least that version. The first fetch may download that toolchain.

To add the command to your module, run:

```bash
go get -tool github.com/lewtec/tailgopher/cmd/tailwind@latest
```

`go.mod` then contains a line like this:

```
tool github.com/lewtec/tailgopher/cmd/tailwind
```

To compile CSS from anywhere inside that module, run:

```bash
go tool tailwind -i input.css -o output.css
```

The last path segment is `tailwind`, so `go tool tailwind` is enough when no
other recorded tool uses that name. The full package path also works:

```bash
go tool github.com/lewtec/tailgopher/cmd/tailwind -i input.css -o output.css
```

To list recorded tools, run `go tool` with no arguments.

To upgrade the recorded command later, run:

```bash
go get -tool github.com/lewtec/tailgopher/cmd/tailwind@latest
```

To remove it, run:

```bash
go get -tool github.com/lewtec/tailgopher/cmd/tailwind@none
```

### Generate CSS from go generate

A typical module layout is:

```
myapp/
  go.mod
  generate.go
  ui/
    input.css
    index.html
    out.css
```

`generate.go` at the module root:

```go
package myapp

//go:generate go tool tailwind -i ./ui/input.css -o ./ui/out.css --minify
```

Then run `go generate ./...` from the module root.

`go generate` starts the command in the directory of the source file.
With `generate.go` at the module root, `-i` and `-o` are relative to that root.

Commit `out.css`, or run `go generate` in CI before you ship the stylesheet.

### Rebuild when files change

Native `@parcel/watcher` does not load in this host. Use polling:

```bash
go tool tailwind -i input.css -o output.css --watch --poll
```

## Write input CSS

The command accepts official Tailwind v4 CSS. A minimal file is:

```css
@import "tailwindcss";
@source "./index.html";
```

`@import "tailwindcss"` loads the official theme and utilities from the
embedded bundle. `@source` names files the scanner reads for class candidates.
Paths are relative to the input CSS file.

The command writes one output CSS path (`-o`). The default is stdout (`-`).

For the CSS dialect, read the [Tailwind CSS documentation](https://tailwindcss.com/docs).

## Catalog plugins

The command contains a closed catalog. These `@plugin` names resolve:

| Name in CSS | What it is |
| --- | --- |
| `daisyui` | [daisyUI](https://daisyui.com) |
| `daisyui/theme` | daisyUI theme tokens |
| `@tailwindcss/typography` | official [Typography](https://github.com/tailwindlabs/tailwindcss-typography) plugin |

Example:

```css
@import "tailwindcss";
@plugin "daisyui";
@plugin "@tailwindcss/typography";
@source "./index.html";
```

A name outside the catalog fails the same way official `@tailwindcss/cli`
fails an unresolved plugin. The command does not read your `node_modules`.

## Relation to Tailwind CSS

<p align="center">
  <img src="docs/assets/tailwindcss-mark.svg" width="72" alt="Official Tailwind CSS mark">
</p>

tailgopher does not reimplement the Tailwind design system. It hosts the
official compiler so a Go module can run it as `go tool tailwind`.

| Job | What runs |
| --- | --- |
| Flags, `@import`, `@source`, `@plugin` | official `@tailwindcss/cli` |
| Class scan | official Oxide (WASM) |
| `--minify` and `--optimize` | official Lightning CSS (WASM) |
| Process | one Go executable. No `node` on `PATH` |

The teal gopher at the top is this project's mark, from
[issue #2](https://github.com/lewtec/tailgopher/issues/2).
The hair follows the official Tailwind wave.

The mark in this section is the official Tailwind CSS mark from the
[Tailwind brand page](https://tailwindcss.com/brand). Tailwind name and logos
are trademarks of Tailwind Labs Inc. Do not read this repo as an official
Tailwind Labs release. Do not use those marks in a way that implies endorsement.

## Command flags

These flags come from official `@tailwindcss/cli`. This command does not add
a second flag dialect.

| Flag | Meaning |
| --- | --- |
| `-i`, `--input` | Input CSS file |
| `-o`, `--output` | Output CSS file. Default: stdout (`-`) |
| `-w`, `--watch[=always]` | Rebuild when sources change. Prefer `--poll` in this host |
| `--poll[=ms]` | Watch by polling instead of filesystem events |
| `-m`, `--minify` | Optimize and minify the output CSS |
| `--optimize` | Optimize without minify |
| `--cwd` | Working directory. Default: `.` |
| `--map` | Write a source map |
| `--silent` | Suppress non-error output |
| `-h`, `--help` | Print official usage text |

Unknown flags and missing input fail with official CLI stderr and a non-zero exit.

## Run with go run

To compile without a `tool` line in `go.mod`, run:

```bash
go run github.com/lewtec/tailgopher/cmd/tailwind@latest -i input.css -o output.css
```

`go run` fetches the module into the module cache and runs it. It does not
install a command onto your `PATH`.

## Pin with mise

[mise](https://mise.jdx.dev/) can pin the command in the project. Do not use
`mise use -g`. That writes a user-global pin.

Add this to the project `mise.toml`:

```toml
[tools]
"go:github.com/lewtec/tailgopher/cmd/tailwind" = "latest"
```

Then run:

```bash
mise install
mise exec -- tailwind -i input.css -o output.css
```

mise installs into its own tool store for this project. After `mise install`,
a mise-activated shell in this directory also has `tailwind` on `PATH`.

A task can call `go tool` or `go run` instead of pinning the command:

```toml
[tasks.css]
run = "go tool tailwind -i ./ui/input.css -o ./ui/out.css --minify"
```

Then run `mise run css`. See the [mise Go backend](https://mise.jdx.dev/dev-tools/backends/go.html).

## Limits

- The process does not start `node`, `bun`, or `deno`.
- The catalog is the only `@plugin` source. Operator `node_modules` is ignored.
- Tailwind v3 JS config is out of scope.
- There is no importable Go `Compile` API yet.

`SPEC.md` is the project specification.

## Maintain this repo

Node and npm are for maintainers of this repo. Operators of the command do not
need them.

1. Install pinned tools with [mise](https://mise.jdx.dev/).
2. Run `npm ci`.
3. Run `go generate ./...` to refresh the embedded bundle and WASM.
4. Run `go test ./...`.

`mise run build` writes `bin/tailwind`. `mise run ci` runs the checks this repo
uses before a release.

## Terms

| Concept | Term this repo uses |
| --- | --- |
| Person who runs `tailwind` | operator |
| Person who changes this repo | maintainer |
| Official Tailwind v4 compile step | compile |
| Closed set of `@plugin` names | catalog |
| JS file `go generate` writes | bundle |
| Stylesheet the command writes | output CSS |
