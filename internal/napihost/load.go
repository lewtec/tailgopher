package napihost

import (
	"context"
	"fmt"
	"os"

	"github.com/dop251/goja"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// LoadLightning instantiates official lightningcss-wasm and returns the JS module.
func LoadLightning(vm *goja.Runtime, lightningWASM []byte) (*goja.Object, error) {
	if len(lightningWASM) == 0 {
		return nil, fmt.Errorf("lightningcss.wasm missing; run go generate")
	}
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	e := newEnv(vm)
	if err := attachNapi(r, e); err != nil {
		return nil, err
	}
	compiled, err := r.CompileModule(ctx, lightningWASM)
	if err != nil {
		return nil, err
	}
	mod, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("lightningcss"))
	if err != nil {
		return nil, err
	}
	e.mod = mod
	e.mem = mod.Memory()
	exports := vm.NewObject()
	handle := e.push(exports)
	if fn := mod.ExportedFunction("register_module"); fn != nil {
		if _, err := fn.Call(ctx); err != nil {
			return nil, fmt.Errorf("register_module: %w", err)
		}
	}
	if fn := mod.ExportedFunction("napi_register_wasm_v1"); fn != nil {
		if _, err := fn.Call(ctx, 0, uint64(handle)); err != nil {
			return nil, fmt.Errorf("napi_register_wasm_v1: %w", err)
		}
	} else if fn := mod.ExportedFunction("napi_register_module_v1"); fn != nil {
		if _, err := fn.Call(ctx, 0, uint64(handle)); err != nil {
			return nil, fmt.Errorf("napi_register_module_v1: %w", err)
		}
	}
	if e.except {
		return nil, fmt.Errorf("lightning register: %v", e.pending)
	}
	return exports, nil
}

// LoadOxide instantiates official Oxide WASI and returns the JS module.
func LoadOxide(vm *goja.Runtime, oxideWASM []byte) (*goja.Object, error) {
	if len(oxideWASM) == 0 {
		return nil, fmt.Errorf("oxide.wasm missing; run go generate")
	}
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2|experimental.CoreFeaturesThreads))
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	e := newEnv(vm)
	if err := attachNapi(r, e); err != nil {
		return nil, err
	}
	// thread-spawn lives in module "wasi"
	if _, err := r.NewHostModuleBuilder("wasi").
		NewFunctionBuilder().
		WithFunc(func(int32) int32 { return -1 }).
		Export("thread-spawn").
		Instantiate(ctx); err != nil {
		return nil, err
	}
	compiled, err := r.CompileModule(ctx, oxideWASM)
	if err != nil {
		return nil, err
	}
	cfg := wazero.NewModuleConfig().
		WithName("oxide").
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithFSConfig(wazero.NewFSConfig().WithDirMount("/", "/"))
	mod, err := r.InstantiateModule(ctx, compiled, cfg)
	if err != nil {
		return nil, err
	}
	e.mod = mod
	e.mem = mod.Memory()
	if init := mod.ExportedFunction("_initialize"); init != nil {
		if _, err := init.Call(ctx); err != nil {
			return nil, fmt.Errorf("oxide _initialize: %w", err)
		}
	}
	exports := vm.NewObject()
	handle := e.push(exports)
	if fn := mod.ExportedFunction("napi_register_wasm_v1"); fn != nil {
		if _, err := fn.Call(ctx, 0, uint64(handle)); err != nil {
			return nil, fmt.Errorf("oxide register: %w", err)
		}
	}
	if e.except {
		return nil, fmt.Errorf("oxide register: %v", e.pending)
	}
	return exports, nil
}
