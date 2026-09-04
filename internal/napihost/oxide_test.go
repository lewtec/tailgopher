package napihost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/buffer"
	"github.com/dop251/goja_nodejs/require"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

func TestOxideImports(t *testing.T) {
	wasm, err := os.ReadFile("../engine/wasm/oxide.wasm")
	if err != nil {
		t.Skip(err)
	}
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2|experimental.CoreFeaturesThreads))
	defer r.Close(ctx)
	compiled, err := r.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range compiled.ImportedFunctions() {
		mod, name, ok := fn.Import()
		if !ok {
			continue
		}
		t.Logf("%s.%s %v -> %v", mod, name, fn.ParamTypes(), fn.ResultTypes())
	}
	for _, mem := range compiled.ImportedMemories() {
		mod, name, ok := mem.Import()
		if !ok {
			continue
		}
		max, _ := mem.Max()
		t.Logf("MEM %s.%s min=%d max=%d", mod, name, mem.Min(), max)
	}
	for name, fn := range compiled.ExportedFunctions() {
		t.Logf("export %s %v -> %v", name, fn.ParamTypes(), fn.ResultTypes())
	}
}

func TestOxideLoad(t *testing.T) {
	wasm, err := os.ReadFile("../engine/wasm/oxide.wasm")
	if err != nil {
		t.Skip(err)
	}
	vm := goja.New()
	require.NewRegistry().Enable(vm)
	buffer.Enable(vm)
	mod, err := LoadOxide(vm, wasm)
	if err != nil {
		t.Fatal(err)
	}
	if mod.Get("Scanner") == nil || goja.IsUndefined(mod.Get("Scanner")) {
		t.Fatalf("oxide loaded without Scanner, keys=%v except=%v", mod.Keys(), err)
	}
}

func TestOxideScan(t *testing.T) {
	t.Skip("official scan deadlocks on WASI threads/rayon atomic.wait")
	wasm, err := os.ReadFile("../engine/wasm/oxide.wasm")
	if err != nil {
		t.Skip(err)
	}
	vm := goja.New()
	require.NewRegistry().Enable(vm)
	buffer.Enable(vm)
	mod, err := LoadOxide(vm, wasm)
	if err != nil {
		t.Fatal(err)
	}
	base, err := filepath.Abs(filepath.Join("..", "..", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	opts := vm.NewObject()
	src := vm.NewObject()
	_ = src.Set("base", base)
	_ = src.Set("pattern", "*.html")
	_ = src.Set("negated", false)
	_ = opts.Set("sources", []any{src})
	inst, err := vm.New(mod.Get("Scanner"), opts)
	if err != nil {
		t.Fatal(err)
	}
	scan, ok := goja.AssertFunction(inst.ToObject(vm).Get("scan"))
	if !ok {
		t.Fatal("scan missing")
	}
	ret, err := scan(inst)
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(ret.Export())
	if !strings.Contains(got, "btn") && !strings.Contains(got, "text-3xl") && !strings.Contains(got, "prose") {
		t.Fatalf("scan() = %s; want candidates from testdata/index.html", got)
	}
}
