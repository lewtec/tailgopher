package engine

import (
	_ "embed"
	"log/slog"

	"github.com/dop251/goja"
	"github.com/lewtec/tailgopher/internal/napihost"
)

//go:embed wasm/lightningcss.wasm
var lightningWASM []byte

//go:embed wasm/oxide.wasm
var oxideWASM []byte

func loadLightningMod(vm *goja.Runtime) (*goja.Object, error) {
	mod, err := napihost.LoadLightning(vm, lightningWASM)
	if err != nil {
		slog.Warn("official lightningcss wasm not loaded", "err", err)
		return nil, err
	}
	if fn := mod.Get("transform"); fn != nil && !goja.IsUndefined(fn) {
		_ = vm.Set("__tw_lightning", fn)
		slog.Info("official lightningcss wasm ready")
	} else {
		slog.Warn("lightningcss wasm loaded without transform export", "keys", mod.Keys())
	}
	return mod, nil
}

func loadOxideMod(vm *goja.Runtime) (*goja.Object, error) {
	mod, err := napihost.LoadOxide(vm, oxideWASM)
	if err != nil {
		slog.Warn("official oxide wasm not loaded", "err", err)
		return nil, err
	}
	return mod, nil
}
