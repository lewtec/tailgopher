package napihost

import (
	"os"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/buffer"
	"github.com/dop251/goja_nodejs/require"
)

func TestLightningTransform(t *testing.T) {
	wasm, err := os.ReadFile("../engine/wasm/lightningcss.wasm")
	if err != nil {
		t.Skip(err)
	}
	vm := goja.New()
	require.NewRegistry().Enable(vm)
	buffer.Enable(vm)

	mod, err := LoadLightning(vm, wasm)
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := goja.AssertFunction(mod.Get("transform"))
	if !ok {
		t.Fatalf("no transform export, keys=%v", mod.Keys())
	}

	css := ".foo { color: red; }"
	opts := vm.NewObject()
	_ = opts.Set("filename", "t.css")
	_ = opts.Set("code", buffer.WrapBytes(vm, []byte(css)))
	_ = opts.Set("minify", true)
	_ = opts.Set("errorRecovery", true)

	ret, err := fn(goja.Undefined(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got := bufferString(vm, ret)
	if !strings.Contains(got, ".foo") {
		t.Fatalf("transform(%q) = %q; want CSS containing .foo", css, got)
	}
}

func bufferString(vm *goja.Runtime, ret goja.Value) string {
	if ret == nil || goja.IsUndefined(ret) || goja.IsNull(ret) {
		return ""
	}
	obj := ret.ToObject(vm)
	if obj == nil {
		return ret.String()
	}
	code := obj.Get("code")
	if code == nil || goja.IsUndefined(code) {
		return obj.String()
	}
	if o := code.ToObject(vm); o != nil {
		if toString, ok := goja.AssertFunction(o.Get("toString")); ok {
			s, err := toString(code)
			if err == nil {
				return s.String()
			}
		}
	}
	if b := asBytes(code); b != nil {
		return string(b)
	}
	return code.String()
}
