package engine

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
	goutil "github.com/dop251/goja_nodejs/util"
)

func registerNodeModules(reg *require.Registry) {
	reg.RegisterNativeModule("path", loadPath)
	reg.RegisterNativeModule("node:path", loadPath)
	reg.RegisterNativeModule("fs", loadFS)
	reg.RegisterNativeModule("node:fs", loadFS)
	reg.RegisterNativeModule("fs/promises", loadFSPromises)
	reg.RegisterNativeModule("node:fs/promises", loadFSPromises)
	reg.RegisterNativeModule("module", loadModule)
	reg.RegisterNativeModule("node:module", loadModule)
	reg.RegisterNativeModule("url", loadURL)
	reg.RegisterNativeModule("node:url", loadURL)
	reg.RegisterNativeModule("util", goutil.Require)
	reg.RegisterNativeModule("node:util", loadUtil)
	reg.RegisterNativeModule("readline", loadReadline)
	reg.RegisterNativeModule("node:readline", loadReadline)
	reg.RegisterNativeModule("process", loadProcess)
	reg.RegisterNativeModule("node:process", loadProcess)
}

func registerOfficialWASM(reg *require.Registry, vm *goja.Runtime) {
	if lc, err := loadLightningMod(vm); err == nil && lc != nil {
		reg.RegisterNativeModule("lightningcss", func(_ *goja.Runtime, module *goja.Object) {
			module.Set("exports", lc)
		})
	}
	if ox, err := loadOxideMod(vm); err == nil && ox != nil {
		reg.RegisterNativeModule("@tailwindcss/oxide", func(_ *goja.Runtime, module *goja.Object) {
			module.Set("exports", ox)
		})
	}
}

func loadProcess(vm *goja.Runtime, module *goja.Object) {
	module.Set("exports", vm.Get("process"))
}

func loadPath(vm *goja.Runtime, module *goja.Object) {
	exp := exports(vm, module)
	fillPath(vm, exp, os.PathSeparator)
	posix := vm.NewObject()
	fillPath(vm, posix, '/')
	win32 := vm.NewObject()
	fillPath(vm, win32, '\\')
	exp.Set("posix", posix)
	exp.Set("win32", win32)
}

func fillPath(vm *goja.Runtime, exp *goja.Object, sep rune) {
	sepStr := string(sep)
	join := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, a := range call.Arguments {
			parts = append(parts, a.String())
		}
		joined := filepath.Join(parts...)
		if sep == '/' {
			joined = filepath.ToSlash(joined)
		}
		return vm.ToValue(joined)
	}
	exp.Set("sep", sepStr)
	exp.Set("delimiter", string(os.PathListSeparator))
	exp.Set("resolve", func(call goja.FunctionCall) goja.Value {
		wd, _ := os.Getwd()
		out := wd
		for _, a := range call.Arguments {
			p := a.String()
			if filepath.IsAbs(p) {
				out = filepath.Clean(p)
				continue
			}
			out = filepath.Clean(filepath.Join(out, p))
		}
		if sep == '/' {
			out = filepath.ToSlash(out)
		}
		return vm.ToValue(out)
	})
	exp.Set("dirname", func(p string) string {
		out := filepath.Dir(p)
		if sep == '/' {
			return filepath.ToSlash(out)
		}
		return out
	})
	exp.Set("basename", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(filepath.Base(call.Argument(0).String()))
	})
	exp.Set("join", join)
	exp.Set("relative", func(from, to string) (string, error) {
		rel, err := filepath.Rel(from, to)
		if sep == '/' {
			rel = filepath.ToSlash(rel)
		}
		return rel, err
	})
	exp.Set("extname", filepath.Ext)
	exp.Set("isAbsolute", filepath.IsAbs)
	exp.Set("normalize", func(p string) string {
		out := filepath.Clean(p)
		if sep == '/' {
			return filepath.ToSlash(out)
		}
		return out
	})
}

func loadFS(vm *goja.Runtime, module *goja.Object) {
	exp := exports(vm, module)
	exp.Set("existsSync", func(p string) bool {
		if _, ok := virtualFile(vm, p); ok {
			return true
		}
		_, err := os.Stat(p)
		return err == nil
	})
	exp.Set("readFileSync", func(call goja.FunctionCall) goja.Value {
		p := call.Argument(0).String()
		if body, ok := virtualFile(vm, p); ok {
			return vm.ToValue(body)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return vm.ToValue(string(b))
	})
	exp.Set("writeFileSync", func(p, data string) {
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			panic(vm.ToValue(err.Error()))
		}
	})
	exp.Set("mkdirSync", func(call goja.FunctionCall) goja.Value {
		p := call.Argument(0).String()
		if err := os.MkdirAll(p, 0o755); err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return goja.Undefined()
	})
	exp.Set("readdirSync", func(call goja.FunctionCall) goja.Value {
		p := call.Argument(0).String()
		entries, err := os.ReadDir(p)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		withTypes := false
		if len(call.Arguments) > 1 {
			if obj := call.Argument(1).ToObject(vm); obj != nil {
				if v := obj.Get("withFileTypes"); v != nil && v.ToBoolean() {
					withTypes = true
				}
			}
		}
		if !withTypes {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			return vm.ToValue(names)
		}
		arr := make([]any, 0, len(entries))
		for _, e := range entries {
			ent := e
			arr = append(arr, map[string]any{
				"name": ent.Name(),
				"isDirectory": func() bool {
					return ent.IsDir()
				},
				"isFile": func() bool {
					return ent.Type().IsRegular() || (!ent.IsDir() && ent.Type()&os.ModeSymlink == 0)
				},
				"isSymbolicLink": func() bool {
					return ent.Type()&os.ModeSymlink != 0
				},
			})
		}
		return vm.ToValue(arr)
	})
	exp.Set("statSync", func(p string) map[string]any {
		m, err := statMap(p, false)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return m
	})
	exp.Set("lstatSync", func(p string) map[string]any {
		m, err := statMap(p, true)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return m
	})
	exp.Set("realpathSync", func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return r
	})
	exp.Set("promises", fsPromises(vm))
}

func loadFSPromises(vm *goja.Runtime, module *goja.Object) {
	module.Set("exports", fsPromises(vm))
}

func fsPromises(vm *goja.Runtime) *goja.Object {
	o := vm.NewObject()
	o.Set("readFile", func(call goja.FunctionCall) goja.Value {
		return promiseOf(vm, func() (any, error) {
			p := call.Argument(0).String()
			if body, ok := virtualFile(vm, p); ok {
				return body, nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			return string(b), nil
		})
	})
	o.Set("writeFile", func(p, data string) goja.Value {
		return promiseOf(vm, func() (any, error) {
			return nil, os.WriteFile(p, []byte(data), 0o644)
		})
	})
	o.Set("mkdir", func(call goja.FunctionCall) goja.Value {
		return promiseOf(vm, func() (any, error) {
			return nil, os.MkdirAll(call.Argument(0).String(), 0o755)
		})
	})
	o.Set("stat", func(p string) goja.Value {
		return promiseOf(vm, func() (any, error) {
			return statMap(p, false)
		})
	})
	o.Set("lstat", func(p string) goja.Value {
		return promiseOf(vm, func() (any, error) {
			return statMap(p, true)
		})
	})
	o.Set("realpath", func(p string) goja.Value {
		return promiseOf(vm, func() (any, error) {
			return filepath.EvalSymlinks(p)
		})
	})
	return o
}

func statMap(p string, lstat bool) (map[string]any, error) {
	var info os.FileInfo
	var err error
	if lstat {
		info, err = os.Lstat(p)
	} else {
		info, err = os.Stat(p)
	}
	if err != nil {
		return nil, err
	}
	mode := info.Mode()
	return map[string]any{
		"isFile":            func() bool { return info.Mode().IsRegular() },
		"isDirectory":       func() bool { return info.IsDir() },
		"isSymbolicLink":    func() bool { return mode&os.ModeSymlink != 0 },
		"isCharacterDevice": func() bool { return mode&os.ModeCharDevice != 0 },
		"isFIFO":            func() bool { return mode&os.ModeNamedPipe != 0 },
		"mtimeMs":           float64(info.ModTime().UnixMilli()),
		"size":              info.Size(),
	}, nil
}

func loadModule(vm *goja.Runtime, module *goja.Object) {
	exp := exports(vm, module)
	exp.Set("createRequire", func(goja.FunctionCall) goja.Value {
		req := vm.Get("require")
		if req == nil {
			return vm.ToValue(func(string) any { return nil })
		}
		obj := req.ToObject(vm)
		_ = obj.Set("resolve", func(id string) string {
			if resolved := callResolve(vm, id); resolved != "" {
				return resolved
			}
			return id
		})
		return req
	})
	exp.Set("registerHooks", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	exp.Set("register", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	exp.Set("isBuiltin", func(id string) bool {
		return strings.HasPrefix(id, "node:") || id == "fs" || id == "path" || id == "module" || id == "url" || id == "util" || id == "readline"
	})
}

func loadURL(vm *goja.Runtime, module *goja.Object) {
	patchURLExports(vm, exports(vm, module))
}

func wrapRequire(vm *goja.Runtime) {
	orig, ok := goja.AssertFunction(vm.Get("require"))
	if !ok {
		return
	}
	_ = vm.Set("require", func(call goja.FunctionCall) goja.Value {
		v, err := orig(goja.Undefined(), call.Arguments...)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		if len(call.Arguments) > 0 {
			id := call.Argument(0).String()
			if id == "url" || id == "node:url" {
				if obj := v.ToObject(vm); obj != nil {
					patchURLExports(vm, obj)
				}
			}
		}
		return v
	})
}

func patchURLModule(vm *goja.Runtime) {
	mod := require.Require(vm, "url")
	if obj, ok := mod.(*goja.Object); ok {
		patchURLExports(vm, obj)
	}
}

func patchURLExports(vm *goja.Runtime, exp *goja.Object) {
	exp.Set("pathToFileURL", func(p string) map[string]any {
		href := "file://" + filepath.ToSlash(p)
		return map[string]any{"href": href, "pathname": filepath.ToSlash(p)}
	})
	exp.Set("fileURLToPath", func(call goja.FunctionCall) goja.Value {
		arg := call.Argument(0)
		if obj := arg.ToObject(vm); obj != nil {
			if p := obj.Get("pathname"); p != nil && !goja.IsUndefined(p) && !goja.IsNull(p) {
				return vm.ToValue(p.String())
			}
			if href := obj.Get("href"); href != nil && !goja.IsUndefined(href) && !goja.IsNull(href) {
				return vm.ToValue(strings.TrimPrefix(href.String(), "file://"))
			}
		}
		return vm.ToValue(strings.TrimPrefix(strings.TrimPrefix(arg.String(), "file://"), "file:"))
	})
}

func loadUtil(vm *goja.Runtime, module *goja.Object) {
	goutil.Require(vm, module)
	exp := exports(vm, module)
	exp.Set("stripVTControlCharacters", func(s string) string {
		return s
	})
	exp.Set("promisify", func(fn goja.Value) goja.Value { return fn })
	exp.Set("deprecate", func(fn goja.Value) goja.Value { return fn })
}

func patchUtilModule(vm *goja.Runtime) {
	mod := require.Require(vm, "util")
	if obj, ok := mod.(*goja.Object); ok && obj != nil {
		_ = obj.Set("stripVTControlCharacters", func(s string) string { return s })
		_ = obj.Set("deprecate", func(fn goja.Value) goja.Value { return fn })
		_ = obj.Set("promisify", func(fn goja.Value) goja.Value { return fn })
		_ = obj.Set("inherits", func(ctor, super goja.Value) {
			// no-op for host
		})
	}
}

func loadReadline(vm *goja.Runtime, module *goja.Object) {
	exp := exports(vm, module)
	exp.Set("createInterface", func(goja.FunctionCall) goja.Value {
		return vm.NewObject()
	})
}

func exports(vm *goja.Runtime, module *goja.Object) *goja.Object {
	v := module.Get("exports")
	if obj, ok := v.(*goja.Object); ok && obj != nil {
		return obj
	}
	o := vm.NewObject()
	module.Set("exports", o)
	return o
}

func promiseOf(vm *goja.Runtime, fn func() (any, error)) goja.Value {
	p, resolve, reject := vm.NewPromise()
	v, err := fn()
	if err != nil {
		_ = reject(err.Error())
	} else {
		_ = resolve(v)
	}
	return vm.ToValue(p)
}

func attachWebGlobals(vm *goja.Runtime) {
	_ = vm.Set("atob", func(s string) string {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return string(b)
	})
	_ = vm.Set("btoa", func(s string) string {
		return base64.StdEncoding.EncodeToString([]byte(s))
	})
}

func attachProcess(vm *goja.Runtime, args []string) error {
	proc := vm.NewObject()
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	argv := append([]string{"node", "tailwind"}, args...)
	env := map[string]string{}
	for _, e := range os.Environ() {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	proc.Set("argv", argv)
	proc.Set("env", env)
	proc.Set("cwd", func() string { return wd })
	proc.Set("exit", func(call goja.FunctionCall) goja.Value {
		code := 0
		if len(call.Arguments) > 0 {
			code = int(call.Arguments[0].ToInteger())
		}
		os.Exit(code)
		return goja.Undefined()
	})
	proc.Set("stdout", map[string]any{
		"write": func(s string) bool {
			_, _ = fmt.Fprint(os.Stdout, s)
			return true
		},
		"isTTY":   isatty(os.Stdout),
		"columns": 80,
	})
	proc.Set("stderr", map[string]any{
		"write": func(s string) bool {
			_, _ = fmt.Fprint(os.Stderr, s)
			return true
		},
		"isTTY":   isatty(os.Stderr),
		"columns": 80,
	})
	proc.Set("stdin", map[string]any{
		"on":     func(goja.FunctionCall) goja.Value { return goja.Undefined() },
		"resume": func(goja.FunctionCall) goja.Value { return goja.Undefined() },
		"isTTY":  false,
	})
	proc.Set("hrtime", hrtime(vm))
	proc.Set("versions", map[string]any{"node": "22.0.0"})
	proc.Set("version", "v22.0.0")
	proc.Set("platform", runtimeGOOS())
	proc.Set("arch", runtimeGOARCH())
	proc.Set("execPath", os.Args[0])
	proc.Set("on", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	proc.Set("off", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	vm.Set("process", proc)
	return nil
}

func hrtime(vm *goja.Runtime) goja.Value {
	start := time.Now()
	fn := vm.ToValue(func() []int64 {
		d := time.Since(start)
		return []int64{int64(d.Seconds()), int64(d.Nanoseconds() % 1e9)}
	}).ToObject(vm)
	fn.Set("bigint", func() int64 {
		return time.Since(start).Nanoseconds()
	})
	return fn
}

func isatty(*os.File) bool { return false }

func runtimeGOOS() string { return runtime.GOOS }

func runtimeGOARCH() string {
	if runtime.GOARCH == "amd64" {
		return "x64"
	}
	return runtime.GOARCH
}

func callResolve(vm *goja.Runtime, id string) string {
	fn, ok := goja.AssertFunction(vm.Get("__tw_resolve"))
	if !ok {
		return ""
	}
	v, err := fn(goja.Undefined(), vm.ToValue(id))
	if err != nil || v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	return v.String()
}

func virtualFile(vm *goja.Runtime, p string) (string, bool) {
	key := strings.TrimPrefix(p, "/virtual/")
	files := vm.Get("__tw_files")
	if files == nil || goja.IsUndefined(files) || goja.IsNull(files) {
		return "", false
	}
	obj := files.ToObject(vm)
	if obj == nil {
		return "", false
	}
	v := obj.Get(key)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return "", false
	}
	return v.String(), true
}
