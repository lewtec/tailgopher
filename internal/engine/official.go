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
	if v := mod.Get("Features"); v == nil || goja.IsUndefined(v) {
		_ = mod.Set("Features", lightningFeatures(vm))
	}
	return mod, nil
}

func lightningFeatures(vm *goja.Runtime) *goja.Object {
	o := vm.NewObject()
	for name, bit := range map[string]int64{
		"Nesting":                        1,
		"NotSelectorList":                2,
		"DirSelector":                    4,
		"LangSelectorList":               8,
		"IsSelector":                     16,
		"TextDecorationThicknessPercent": 32,
		"MediaIntervalSyntax":            64,
		"MediaRangeSyntax":               128,
		"CustomMediaQueries":             256,
		"ClampFunction":                  512,
		"ColorFunction":                  1024,
		"OklabColors":                    2048,
		"LabColors":                      4096,
		"P3Colors":                       8192,
		"HexAlphaColors":                 16384,
		"SpaceSeparatedColorNotation":    32768,
		"FontFamilySystemUi":             65536,
		"DoublePositionGradients":        131072,
		"VendorPrefixes":                 262144,
		"LogicalProperties":              524288,
		"LightDark":                      1048576,
		"Selectors":                      31,
		"MediaQueries":                   448,
		"Colors":                         1113088,
	} {
		_ = o.Set(name, bit)
	}
	return o
}

func loadOxideMod(vm *goja.Runtime) (*goja.Object, error) {
	mod, err := napihost.LoadOxide(vm, oxideWASM)
	if err != nil {
		slog.Warn("official oxide wasm not loaded", "err", err)
		return nil, err
	}
	if sc := mod.Get("Scanner"); sc != nil && !goja.IsUndefined(sc) {
		_ = vm.Set("__tw_oxide_scanner", sc)
		slog.Info("official oxide wasm ready")
	} else {
		slog.Warn("oxide wasm loaded without Scanner", "keys", mod.Keys())
	}
	return mod, nil
}
