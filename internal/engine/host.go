package engine

import (
	"fmt"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/buffer"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/dop251/goja_nodejs/require"
	"github.com/dop251/goja_nodejs/url"
)

// Run executes official @tailwindcss/cli with the given argv.
func Run(args []string) error {
	loop := eventloop.NewEventLoop(eventloop.EnableConsole(true))
	var runErr error
	loop.Run(func(vm *goja.Runtime) {
		registry := require.NewRegistry(require.WithLoader(func(string) ([]byte, error) {
			return nil, require.ModuleFileDoesNotExistError
		}))
		if err := attachProcess(vm, args); err != nil {
			runErr = err
			return
		}
		registerNodeModules(registry)
		registry.Enable(vm)
		console.Enable(vm)
		url.Enable(vm)
		buffer.Enable(vm)
		patchURLModule(vm)
		wrapRequire(vm)
		if _, err := vm.RunString(Bundle); err != nil {
			runErr = fmt.Errorf("cli: %w", err)
			return
		}
		done := vm.Get("__tw_done")
		if done == nil || goja.IsUndefined(done) || goja.IsNull(done) {
			return
		}
		if p, ok := done.Export().(*goja.Promise); ok {
			switch p.State() {
			case goja.PromiseStateRejected:
				runErr = fmt.Errorf("cli: %s", jsError(vm, p.Result()))
			case goja.PromiseStatePending:
				runErr = fmt.Errorf("cli: compile still pending")
			}
		}
	})
	return runErr
}

func jsError(vm *goja.Runtime, v goja.Value) string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return "<nil>"
	}
	if obj := v.ToObject(vm); obj != nil {
		if msg := obj.Get("stack"); msg != nil && !goja.IsUndefined(msg) && msg.String() != "" {
			return msg.String()
		}
		if msg := obj.Get("message"); msg != nil && !goja.IsUndefined(msg) && msg.String() != "" {
			return msg.String()
		}
	}
	return v.String()
}
