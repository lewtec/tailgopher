package napihost

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/buffer"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental/table"
)

const (
	napiOK              = 0
	napiInvalidArg      = 1
	napiStringExpected  = 3
	napiNumberExpected  = 6
	napiBooleanExpected = 7
	napiGenericFailure  = 9
	napiPendingExc      = 10

	idxUndefined = 0
	idxNull      = 1
	idxGlobal    = 2
	idxTrue      = 3
	idxFalse     = 4
)

// Env is a Node-API environment backed by goja values and wazero memory.
type Env struct {
	vm           *goja.Runtime
	mod          api.Module
	mem          api.Memory
	scopes       [][]goja.Value
	refs         map[uint32]ref
	nextRef      uint32
	wraps        sync.Map // *goja.Object -> uint64
	pending      goja.Value
	except       bool
	cb           *cbInfo
	bufs         []mappedBuf
	instanceData uint32
	nextTid      atomic.Int32
	rt           wazero.Runtime
	compiled     wazero.CompiledModule
	newCfg       func() wazero.ModuleConfig
	spawnMu      sync.Mutex
}

func (e *Env) spawnThread(startArg int32) int32 {
	if e.rt == nil || e.compiled == nil || e.newCfg == nil {
		return -6
	}
	id := e.nextTid.Add(1)
	if id <= 0 {
		id = e.nextTid.Add(1)
	}
	started := make(chan error, 1)
	go func() {
		ctx := context.Background()
		e.spawnMu.Lock()
		inst, err := e.rt.InstantiateModule(ctx, e.compiled, e.newCfg().
			WithName(fmt.Sprintf("oxide-t-%d", id)).
			WithStartFunctions())
		e.spawnMu.Unlock()
		if err != nil {
			started <- err
			return
		}
		defer inst.Close(ctx)
		started <- nil
		fn := inst.ExportedFunction("wasi_thread_start")
		if fn == nil {
			slog.Error("oxide thread missing wasi_thread_start", "tid", id)
			return
		}
		if _, err := fn.Call(ctx, uint64(id), uint64(startArg)); err != nil {
			slog.Error("oxide wasi_thread_start", "tid", id, "err", err)
		}
	}()
	if err := <-started; err != nil {
		slog.Error("oxide thread instantiate", "tid", id, "err", err)
		return -6
	}
	return id
}

type mappedBuf struct {
	ptr  uint32
	dest []byte
}

type ref struct {
	v     goja.Value
	count uint32
}

type cbInfo struct {
	this   goja.Value
	args   []goja.Value
	data   uint32
	target goja.Value
}

func newEnv(vm *goja.Runtime) *Env {
	e := &Env{vm: vm, refs: map[uint32]ref{}, nextRef: 1}
	e.pushScope()
	return e
}

func (e *Env) pushScope() {
	base := []goja.Value{
		goja.Undefined(),
		goja.Null(),
		e.vm.GlobalObject(),
		e.vm.ToValue(true),
		e.vm.ToValue(false),
	}
	if n := len(e.scopes); n > 0 {
		prev := e.scopes[n-1]
		base = append([]goja.Value{}, prev...)
	}
	e.scopes = append(e.scopes, base)
}

func (e *Env) popScope() {
	e.syncBufs()
	if len(e.scopes) > 1 {
		e.scopes = e.scopes[:len(e.scopes)-1]
	}
}

func (e *Env) syncBufs() {
	if e.mem == nil {
		return
	}
	for _, b := range e.bufs {
		if b.ptr == 0 || len(b.dest) == 0 {
			continue
		}
		if raw, ok := e.mem.Read(b.ptr, uint32(len(b.dest))); ok {
			copy(b.dest, raw)
		}
	}
}

func (e *Env) malloc(n uint32) uint32 {
	if e.mod == nil || n == 0 {
		return 0
	}
	fn := e.mod.ExportedFunction("napi_wasm_malloc")
	if fn == nil {
		fn = e.mod.ExportedFunction("malloc")
	}
	if fn == nil {
		return 0
	}
	res, err := fn.Call(context.Background(), uint64(n))
	if err != nil || len(res) == 0 {
		return 0
	}
	return uint32(res[0])
}

func (e *Env) pinBytes(b []byte) uint32 {
	if len(b) == 0 {
		return 0
	}
	ptr := e.malloc(uint32(len(b)))
	if ptr == 0 {
		return 0
	}
	_ = e.mem.Write(ptr, b)
	e.bufs = append(e.bufs, mappedBuf{ptr: ptr, dest: b})
	return ptr
}

func (e *Env) newBuffer(n, ptr uint32) goja.Value {
	dest := make([]byte, n)
	if ptr != 0 && e.mem != nil {
		if raw, ok := e.mem.Read(ptr, n); ok {
			copy(dest, raw)
		}
	}
	if ptr != 0 {
		e.bufs = append(e.bufs, mappedBuf{ptr: ptr, dest: dest})
	}
	if buf := wrapBytes(e.vm, dest); buf != nil {
		return buf
	}
	o := e.vm.NewObject()
	_ = o.Set("length", int64(n))
	_ = o.Set("toString", func() string {
		e.syncBufs()
		return string(dest)
	})
	return o
}

func wrapBytes(vm *goja.Runtime, dest []byte) (out *goja.Object) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	return buffer.WrapBytes(vm, dest)
}

func (e *Env) cur() []goja.Value { return e.scopes[len(e.scopes)-1] }

func (e *Env) get(idx uint32) goja.Value {
	s := e.cur()
	if int(idx) >= len(s) {
		return goja.Undefined()
	}
	if s[idx] == nil {
		return goja.Undefined()
	}
	return s[idx]
}

func (e *Env) push(v goja.Value) uint32 {
	s := e.cur()
	id := uint32(len(s))
	e.scopes[len(e.scopes)-1] = append(s, v)
	return id
}

func (e *Env) create(v goja.Value, result uint32) uint32 {
	if v == nil || goja.IsUndefined(v) {
		return e.setU32(result, idxUndefined)
	}
	if goja.IsNull(v) {
		return e.setU32(result, idxNull)
	}
	if b, ok := v.Export().(bool); ok {
		if b {
			return e.setU32(result, idxTrue)
		}
		return e.setU32(result, idxFalse)
	}
	return e.setU32(result, e.push(v))
}

func (e *Env) setU32(ptr, v uint32) uint32 {
	if ptr == 0 {
		return napiOK
	}
	_ = e.mem.WriteUint32Le(ptr, v)
	return napiOK
}

func (e *Env) setI32(ptr uint32, v int32) uint32 {
	if ptr == 0 {
		return napiOK
	}
	_ = e.mem.WriteUint32Le(ptr, uint32(v))
	return napiOK
}

func (e *Env) readU32(ptr uint32) uint32 {
	v, _ := e.mem.ReadUint32Le(ptr)
	return v
}

func (e *Env) readString(ptr, length uint32) string {
	if ptr == 0 {
		return ""
	}
	if length == 0 || length == ^uint32(0) {
		// cstring
		var buf []byte
		for i := uint32(0); ; i++ {
			b, ok := e.mem.ReadByte(ptr + i)
			if !ok || b == 0 {
				break
			}
			buf = append(buf, b)
		}
		return string(buf)
	}
	raw, ok := e.mem.Read(ptr, length)
	if !ok {
		return ""
	}
	return string(raw)
}

func (e *Env) writeString(ptr uint32, s string, max uint32) uint32 {
	b := []byte(s)
	n := uint32(len(b))
	if max > 0 && n > max-1 {
		n = max - 1
	}
	if ptr != 0 && n > 0 {
		_ = e.mem.Write(ptr, b[:n])
		_ = e.mem.WriteByte(ptr+n, 0)
	}
	return n
}

func (e *Env) callTable(idx uint32, envID, info uint32) (uint32, error) {
	fn := table.LookupFunction(e.mod, 0, idx,
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
	)
	res, err := fn.Call(context.Background(), uint64(envID), uint64(info))
	if err != nil {
		return 0, err
	}
	if len(res) == 0 {
		return 0, nil
	}
	return uint32(res[0]), nil
}

func (e *Env) invoke(tableIdx, data uint32, this goja.Value, args []goja.Value) goja.Value {
	e.pushScope()
	defer e.popScope()
	e.cb = &cbInfo{this: this, args: append([]goja.Value{}, args...), data: data}
	info := e.push(e.vm.ToValue("cbinfo"))
	ret, err := e.callTable(tableIdx, 0, info)
	if err != nil {
		panic(e.vm.ToValue(err.Error()))
	}
	if e.except {
		ex := e.pending
		e.except = false
		e.pending = nil
		panic(ex)
	}
	return e.get(ret)
}

func (e *Env) makeJSFunc(tableIdx, data uint32) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		return e.invoke(tableIdx, data, call.This, call.Arguments)
	}
}

func (e *Env) makeJSCtor(tableIdx, data uint32) func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		ret := e.invoke(tableIdx, data, call.This, call.Arguments)
		if ret != nil && !goja.IsUndefined(ret) && !goja.IsNull(ret) {
			if obj := ret.ToObject(e.vm); obj != nil {
				return obj
			}
		}
		return call.This
	}
}

func attachNapi(r wazero.Runtime, e *Env, moduleName string) error {
	b := r.NewHostModuleBuilder(moduleName)
	exp := func(name string, fn interface{}) {
		b.NewFunctionBuilder().WithFunc(fn).Export(name)
	}

	exp("napi_create_object", func(_ context.Context, m api.Module, _env uint32, result uint32) uint32 {
		e.mem = m.Memory()
		return e.create(e.vm.NewObject(), result)
	})
	exp("napi_create_array_with_length", func(_ context.Context, m api.Module, _env uint32, n, result uint32) uint32 {
		e.mem = m.Memory()
		arr := e.vm.NewArray(int(n))
		return e.create(arr, result)
	})
	exp("napi_get_undefined", func(_ context.Context, m api.Module, _env uint32, result uint32) uint32 {
		e.mem = m.Memory()
		return e.setU32(result, idxUndefined)
	})
	exp("napi_get_null", func(_ context.Context, m api.Module, _env uint32, result uint32) uint32 {
		e.mem = m.Memory()
		return e.setU32(result, idxNull)
	})
	exp("napi_get_global", func(_ context.Context, m api.Module, _env uint32, result uint32) uint32 {
		e.mem = m.Memory()
		return e.setU32(result, idxGlobal)
	})
	exp("napi_get_boolean", func(_ context.Context, m api.Module, _env uint32, v, result uint32) uint32 {
		e.mem = m.Memory()
		if v != 0 {
			return e.setU32(result, idxTrue)
		}
		return e.setU32(result, idxFalse)
	})
	exp("napi_create_int32", func(_ context.Context, m api.Module, _env uint32, v int32, result uint32) uint32 {
		e.mem = m.Memory()
		return e.create(e.vm.ToValue(v), result)
	})
	exp("napi_create_uint32", func(_ context.Context, m api.Module, _env uint32, v, result uint32) uint32 {
		e.mem = m.Memory()
		return e.create(e.vm.ToValue(v), result)
	})
	exp("napi_create_int64", func(_ context.Context, m api.Module, _env uint32, v int64, result uint32) uint32 {
		e.mem = m.Memory()
		return e.create(e.vm.ToValue(v), result)
	})
	exp("napi_create_double", func(_ context.Context, m api.Module, _env uint32, v float64, result uint32) uint32 {
		e.mem = m.Memory()
		return e.create(e.vm.ToValue(v), result)
	})
	exp("napi_create_string_utf8", func(_ context.Context, m api.Module, _env uint32, ptr, length, result uint32) uint32 {
		e.mem = m.Memory()
		return e.create(e.vm.ToValue(e.readString(ptr, length)), result)
	})
	exp("napi_get_value_string_utf8", func(_ context.Context, m api.Module, _env uint32, value, buf, bufsize, written uint32) uint32 {
		e.mem = m.Memory()
		s := e.get(value).String()
		n := e.writeString(buf, s, bufsize)
		if written != 0 {
			e.setU32(written, n)
		}
		return napiOK
	})
	exp("napi_coerce_to_string", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		return e.create(e.vm.ToValue(e.get(value).String()), result)
	})
	exp("napi_coerce_to_object", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		v := e.get(value)
		if obj := v.ToObject(e.vm); obj != nil {
			return e.create(obj, result)
		}
		return e.create(v, result)
	})
	exp("napi_typeof", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		v := e.get(value)
		var t uint32 = 0 // undefined
		switch {
		case goja.IsUndefined(v):
			t = 0
		case goja.IsNull(v):
			t = 1
		case isBool(v):
			t = 2
		case isNumber(v):
			t = 3
		case isString(v):
			t = 4
		case isSymbol(v):
			t = 5
		case isFunc(v):
			t = 7
		case isObject(v):
			t = 6
		}
		return e.setU32(result, t)
	})
	exp("napi_get_value_bool", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		v := e.get(value).Export()
		b, ok := v.(bool)
		if !ok {
			return napiBooleanExpected
		}
		if b {
			_ = e.mem.WriteByte(result, 1)
		} else {
			_ = e.mem.WriteByte(result, 0)
		}
		return napiOK
	})
	exp("napi_get_value_double", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		f := e.get(value).ToFloat()
		return writeF64(e.mem, result, f)
	})
	exp("napi_set_named_property", func(_ context.Context, m api.Module, _env uint32, object, namePtr, value uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		if obj == nil {
			return napiInvalidArg
		}
		_ = obj.Set(e.readString(namePtr, ^uint32(0)), e.get(value))
		return napiOK
	})
	exp("napi_get_named_property", func(_ context.Context, m api.Module, _env uint32, object, namePtr, result uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		if obj == nil {
			return e.create(goja.Undefined(), result)
		}
		return e.create(obj.Get(e.readString(namePtr, ^uint32(0))), result)
	})
	exp("napi_has_named_property", func(_ context.Context, m api.Module, _env uint32, object, namePtr, result uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		name := e.readString(namePtr, ^uint32(0))
		has := obj != nil && obj.Get(name) != nil && !goja.IsUndefined(obj.Get(name))
		if has {
			_ = e.mem.WriteByte(result, 1)
		} else {
			_ = e.mem.WriteByte(result, 0)
		}
		return napiOK
	})
	exp("napi_set_property", func(_ context.Context, m api.Module, _env uint32, object, key, value uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		if obj == nil {
			return napiInvalidArg
		}
		_ = obj.Set(e.get(key).String(), e.get(value))
		return napiOK
	})
	exp("napi_get_property", func(_ context.Context, m api.Module, _env uint32, object, key, result uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		if obj == nil {
			return e.create(goja.Undefined(), result)
		}
		return e.create(obj.Get(e.get(key).String()), result)
	})
	exp("napi_get_property_names", func(_ context.Context, m api.Module, _env uint32, object, result uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		if obj == nil {
			return e.create(e.vm.NewArray(), result)
		}
		return e.create(e.vm.ToValue(obj.Keys()), result)
	})
	exp("napi_set_element", func(_ context.Context, m api.Module, _env uint32, object, index, value uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		if obj == nil {
			return napiInvalidArg
		}
		_ = obj.Set(fmt.Sprint(index), e.get(value))
		return napiOK
	})
	exp("napi_get_element", func(_ context.Context, m api.Module, _env uint32, object, index, result uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		if obj == nil {
			return e.create(goja.Undefined(), result)
		}
		return e.create(obj.Get(fmt.Sprint(index)), result)
	})
	exp("napi_is_array", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		v := e.get(value)
		ok := false
		if obj, ok2 := v.(*goja.Object); ok2 && obj != nil {
			ok = obj.ClassName() == "Array"
		}
		if ok {
			_ = e.mem.WriteByte(result, 1)
		} else {
			_ = e.mem.WriteByte(result, 0)
		}
		return napiOK
	})
	exp("napi_get_array_length", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(value).ToObject(e.vm)
		n := uint32(0)
		if obj != nil {
			if ln := obj.Get("length"); ln != nil {
				n = uint32(ln.ToInteger())
			}
		}
		return e.setU32(result, n)
	})
	exp("napi_is_typedarray", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		v := 0
		if isTyped(e.get(value)) {
			v = 1
		}
		_ = e.mem.WriteByte(result, byte(v))
		return napiOK
	})
	exp("napi_get_typedarray_info", func(_ context.Context, m api.Module, _env uint32, typedarray, typePtr, lengthPtr, dataPtr, arraybuffer, byteOffset uint32) uint32 {
		e.mem = m.Memory()
		e.mod = m
		b := asBytes(e.get(typedarray))
		ptr := e.pinBytes(b)
		if dataPtr != 0 {
			e.setU32(dataPtr, ptr)
		}
		if lengthPtr != 0 {
			e.setU32(lengthPtr, uint32(len(b)))
		}
		if typePtr != 0 {
			e.setU32(typePtr, 1) // uint8
		}
		if byteOffset != 0 {
			e.setU32(byteOffset, 0)
		}
		if arraybuffer != 0 {
			e.create(e.get(typedarray), arraybuffer)
		}
		return napiOK
	})
	exp("napi_create_buffer", func(_ context.Context, m api.Module, _env uint32, size, dataPtr, result uint32) uint32 {
		e.mem = m.Memory()
		e.mod = m
		ptr := e.malloc(size)
		if dataPtr != 0 {
			e.setU32(dataPtr, ptr)
		}
		return e.create(e.newBuffer(size, ptr), result)
	})
	exp("napi_create_buffer_copy", func(_ context.Context, m api.Module, _env uint32, size, data, resultData, result uint32) uint32 {
		e.mem = m.Memory()
		e.mod = m
		ptr := e.malloc(size)
		if raw, ok := e.mem.Read(data, size); ok && ptr != 0 {
			_ = e.mem.Write(ptr, raw)
		}
		if resultData != 0 {
			e.setU32(resultData, ptr)
		}
		return e.create(e.newBuffer(size, ptr), result)
	})
	exp("napi_create_external_buffer", func(_ context.Context, m api.Module, _env uint32, size, data, finalize, hint, result uint32) uint32 {
		e.mem = m.Memory()
		e.mod = m
		return e.create(e.newBuffer(size, data), result)
	})
	exp("napi_is_buffer", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		v := 0
		if isTyped(e.get(value)) {
			v = 1
		}
		_ = e.mem.WriteByte(result, byte(v))
		return napiOK
	})
	exp("napi_get_buffer_info", func(_ context.Context, m api.Module, _env uint32, value, dataPtr, lengthPtr uint32) uint32 {
		e.mem = m.Memory()
		e.mod = m
		b := asBytes(e.get(value))
		if dataPtr != 0 {
			e.setU32(dataPtr, e.pinBytes(b))
		}
		if lengthPtr != 0 {
			e.setU32(lengthPtr, uint32(len(b)))
		}
		return napiOK
	})
	exp("napi_get_value_int32", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		return e.setI32(result, int32(e.get(value).ToInteger()))
	})
	exp("napi_get_value_uint32", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		return e.setU32(result, uint32(e.get(value).ToInteger()))
	})
	exp("napi_get_value_int64", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		if result != 0 {
			_ = e.mem.WriteUint64Le(result, uint64(e.get(value).ToInteger()))
		}
		return napiOK
	})
	exp("napi_create_array", func(_ context.Context, m api.Module, _env uint32, result uint32) uint32 {
		e.mem = m.Memory()
		return e.create(e.vm.NewArray(), result)
	})
	exp("napi_open_handle_scope", func(_ context.Context, m api.Module, _env uint32, result uint32) uint32 {
		e.mem = m.Memory()
		e.pushScope()
		return e.setU32(result, uint32(len(e.scopes)-1))
	})
	exp("napi_close_handle_scope", func(_ context.Context, m api.Module, _env, scope uint32) uint32 {
		e.popScope()
		return napiOK
	})
	exp("napi_open_escapable_handle_scope", func(_ context.Context, m api.Module, _env uint32, result uint32) uint32 {
		e.mem = m.Memory()
		e.pushScope()
		return e.setU32(result, uint32(len(e.scopes)-1))
	})
	exp("napi_close_escapable_handle_scope", func(_ context.Context, m api.Module, _env, scope uint32) uint32 {
		e.popScope()
		return napiOK
	})
	exp("napi_escape_handle", func(_ context.Context, m api.Module, _env, scope, escapee, result uint32) uint32 {
		e.mem = m.Memory()
		v := e.get(escapee)
		if len(e.scopes) < 2 {
			return e.create(v, result)
		}
		outer := e.scopes[len(e.scopes)-2]
		id := uint32(len(outer))
		e.scopes[len(e.scopes)-2] = append(outer, v)
		return e.setU32(result, id)
	})
	exp("napi_set_instance_data", func(_ context.Context, m api.Module, _env, data, finalize, hint uint32) uint32 {
		e.instanceData = data
		return napiOK
	})
	exp("napi_get_instance_data", func(_ context.Context, m api.Module, _env, result uint32) uint32 {
		e.mem = m.Memory()
		return e.setU32(result, e.instanceData)
	})
	exp("napi_has_property", func(_ context.Context, m api.Module, _env uint32, object, key, result uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		name := e.get(key).String()
		has := obj != nil && obj.Get(name) != nil && !goja.IsUndefined(obj.Get(name))
		if has {
			_ = e.mem.WriteByte(result, 1)
		} else {
			_ = e.mem.WriteByte(result, 0)
		}
		return napiOK
	})
	exp("napi_create_reference", func(_ context.Context, m api.Module, _env uint32, value, refcount, result uint32) uint32 {
		e.mem = m.Memory()
		id := e.nextRef
		e.nextRef++
		e.refs[id] = ref{v: e.get(value), count: refcount}
		return e.setU32(result, id)
	})
	exp("napi_delete_reference", func(_ context.Context, m api.Module, _env uint32, r uint32) uint32 {
		delete(e.refs, r)
		return napiOK
	})
	exp("napi_get_reference_value", func(_ context.Context, m api.Module, _env uint32, r, result uint32) uint32 {
		e.mem = m.Memory()
		rf, ok := e.refs[r]
		if !ok {
			return e.create(goja.Undefined(), result)
		}
		return e.create(rf.v, result)
	})
	exp("napi_reference_unref", func(_ context.Context, m api.Module, _env uint32, r, result uint32) uint32 {
		e.mem = m.Memory()
		rf, ok := e.refs[r]
		if !ok || rf.count == 0 {
			return napiGenericFailure
		}
		rf.count--
		e.refs[r] = rf
		if result != 0 {
			e.setU32(result, rf.count)
		}
		return napiOK
	})
	exp("napi_create_function", func(_ context.Context, m api.Module, _env uint32, namePtr, length, cb, data, result uint32) uint32 {
		e.mem = m.Memory()
		e.mod = m
		fn := e.vm.ToValue(e.makeJSFunc(cb, data))
		return e.create(fn, result)
	})
	exp("napi_define_class", func(_ context.Context, m api.Module, _env uint32, namePtr, length, ctor, data, propCount, props, result uint32) uint32 {
		e.mem = m.Memory()
		e.mod = m
		fn := e.vm.ToValue(e.makeJSCtor(ctor, data))
		obj := fn.ToObject(e.vm)
		proto := e.vm.NewObject()
		if obj != nil {
			_ = obj.Set("prototype", proto)
		}
		const napiStatic = 1 << 10
		for i := uint32(0); i < propCount; i++ {
			base := props + i*32
			utf8 := e.readU32(base)
			nameVal := e.readU32(base + 4)
			method := e.readU32(base + 8)
			attrs := e.readU32(base + 24)
			name := ""
			if utf8 != 0 {
				name = e.readString(utf8, ^uint32(0))
			} else {
				name = e.get(nameVal).String()
			}
			if method == 0 {
				continue
			}
			target := proto
			if attrs&napiStatic != 0 && obj != nil {
				target = obj
			}
			_ = target.Set(name, e.vm.ToValue(e.makeJSFunc(method, e.readU32(base+28))))
		}
		return e.create(fn, result)
	})
	exp("napi_define_properties", func(_ context.Context, m api.Module, _env uint32, object, count, props uint32) uint32 {
		e.mem = m.Memory()
		e.mod = m
		obj := e.get(object).ToObject(e.vm)
		if obj == nil {
			return napiInvalidArg
		}
		// 8 x uint32 per descriptor
		for i := uint32(0); i < count; i++ {
			base := props + i*32
			utf8 := e.readU32(base)
			nameVal := e.readU32(base + 4)
			method := e.readU32(base + 8)
			name := ""
			if utf8 != 0 {
				name = e.readString(utf8, ^uint32(0))
			} else {
				name = e.get(nameVal).String()
			}
			if method != 0 {
				_ = obj.Set(name, e.vm.ToValue(e.makeJSFunc(method, e.readU32(base+28))))
			} else {
				val := e.readU32(base + 20)
				if val != 0 {
					_ = obj.Set(name, e.get(val))
				}
			}
		}
		return napiOK
	})
	exp("napi_get_cb_info", func(_ context.Context, m api.Module, _env uint32, cbinfo, argcPtr, argv, thisArg, dataPtr uint32) uint32 {
		e.mem = m.Memory()
		info := e.cb
		if info == nil {
			return napiInvalidArg
		}
		argc := uint32(len(info.args))
		if argcPtr != 0 {
			want := e.readU32(argcPtr)
			if want < argc {
				argc = want
			}
			e.setU32(argcPtr, uint32(len(info.args)))
		}
		if argv != 0 {
			for i := uint32(0); i < argc; i++ {
				id := e.push(info.args[i])
				e.setU32(argv+i*4, id)
			}
		}
		if thisArg != 0 {
			e.setU32(thisArg, e.push(info.this))
		}
		if dataPtr != 0 {
			e.setU32(dataPtr, info.data)
		}
		return napiOK
	})
	exp("napi_call_function", func(_ context.Context, m api.Module, _env uint32, recv, fn, argc, argv, result uint32) uint32 {
		e.mem = m.Memory()
		f, ok := goja.AssertFunction(e.get(fn))
		if !ok {
			return napiInvalidArg
		}
		args := make([]goja.Value, argc)
		for i := uint32(0); i < argc; i++ {
			args[i] = e.get(e.readU32(argv + i*4))
		}
		ret, err := f(e.get(recv), args...)
		if err != nil {
			e.except = true
			e.pending = e.vm.ToValue(err.Error())
			return napiPendingExc
		}
		return e.create(ret, result)
	})
	exp("napi_new_instance", func(_ context.Context, m api.Module, _env uint32, ctor, argc, argv, result uint32) uint32 {
		e.mem = m.Memory()
		f, ok := goja.AssertFunction(e.get(ctor))
		if !ok {
			return napiInvalidArg
		}
		args := make([]goja.Value, argc)
		for i := uint32(0); i < argc; i++ {
			args[i] = e.get(e.readU32(argv + i*4))
		}
		ret, err := f(goja.Undefined(), args...)
		if err != nil {
			e.except = true
			e.pending = e.vm.ToValue(err.Error())
			return napiPendingExc
		}
		return e.create(ret, result)
	})
	exp("napi_get_prototype", func(_ context.Context, m api.Module, _env uint32, object, result uint32) uint32 {
		e.mem = m.Memory()
		obj := e.get(object).ToObject(e.vm)
		if obj == nil {
			return e.create(goja.Null(), result)
		}
		if proto := obj.Get("prototype"); proto != nil && !goja.IsUndefined(proto) && !goja.IsNull(proto) {
			return e.create(proto, result)
		}
		if proto := obj.Prototype(); proto != nil {
			return e.create(proto, result)
		}
		return e.create(goja.Null(), result)
	})
	exp("napi_strict_equals", func(_ context.Context, m api.Module, _env uint32, lhs, rhs, result uint32) uint32 {
		e.mem = m.Memory()
		eq := e.get(lhs).StrictEquals(e.get(rhs))
		if eq {
			_ = e.mem.WriteByte(result, 1)
		} else {
			_ = e.mem.WriteByte(result, 0)
		}
		return napiOK
	})
	exp("napi_wrap", func(_ context.Context, m api.Module, _env uint32, js, native, finalize, hint, result uint32) uint32 {
		e.mem = m.Memory()
		if obj := e.get(js).ToObject(e.vm); obj != nil {
			e.wraps.Store(obj, uint64(native))
		}
		return napiOK
	})
	exp("napi_unwrap", func(_ context.Context, m api.Module, _env uint32, js, result uint32) uint32 {
		e.mem = m.Memory()
		if obj := e.get(js).ToObject(e.vm); obj != nil {
			if v, ok := e.wraps.Load(obj); ok {
				return e.setU32(result, uint32(v.(uint64)))
			}
		}
		return napiInvalidArg
	})
	exp("napi_is_error", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		_ = e.mem.WriteByte(result, 0)
		return napiOK
	})
	exp("napi_is_promise", func(_ context.Context, m api.Module, _env uint32, value, result uint32) uint32 {
		e.mem = m.Memory()
		_, ok := e.get(value).Export().(*goja.Promise)
		if ok {
			_ = e.mem.WriteByte(result, 1)
		} else {
			_ = e.mem.WriteByte(result, 0)
		}
		return napiOK
	})
	exp("napi_is_exception_pending", func(_ context.Context, m api.Module, _env uint32, result uint32) uint32 {
		e.mem = m.Memory()
		if e.except {
			_ = e.mem.WriteByte(result, 1)
		} else {
			_ = e.mem.WriteByte(result, 0)
		}
		return napiOK
	})
	exp("napi_get_and_clear_last_exception", func(_ context.Context, m api.Module, _env uint32, result uint32) uint32 {
		e.mem = m.Memory()
		ex := e.pending
		e.pending = nil
		e.except = false
		if ex == nil {
			ex = goja.Undefined()
		}
		return e.create(ex, result)
	})
	exp("napi_throw", func(_ context.Context, m api.Module, _env uint32, err uint32) uint32 {
		e.pending = e.get(err)
		e.except = true
		return napiOK
	})
	exp("napi_throw_error", func(_ context.Context, m api.Module, _env uint32, code, msg uint32) uint32 {
		e.mem = m.Memory()
		e.pending = e.vm.ToValue(e.readString(msg, ^uint32(0)))
		e.except = true
		return napiOK
	})
	exp("napi_create_error", func(_ context.Context, m api.Module, _env uint32, code, msg, result uint32) uint32 {
		e.mem = m.Memory()
		return e.create(e.vm.ToValue(e.get(msg).String()), result)
	})
	exp("napi_create_threadsafe_function", func(_ context.Context, m api.Module, _a, _b, _c, _d, _e, _f, _g, _h, _i, _j, _k uint32) uint32 {
		return napiOK
	})
	exp("napi_unref_threadsafe_function", func(_ context.Context, m api.Module, _a, _b uint32) uint32 {
		return napiOK
	})
	exp("napi_ref_threadsafe_function", func(_ context.Context, m api.Module, _a, _b uint32) uint32 {
		return napiOK
	})
	exp("napi_acquire_threadsafe_function", func(_ context.Context, m api.Module, _a, _b uint32) uint32 {
		return napiOK
	})
	exp("napi_release_threadsafe_function", func(_ context.Context, m api.Module, _a, _b, _c uint32) uint32 {
		return napiOK
	})
	exp("napi_call_threadsafe_function", func(_ context.Context, m api.Module, _a, _b, _c uint32) uint32 {
		return napiOK
	})
	exp("napi_get_threadsafe_function_context", func(_ context.Context, m api.Module, _a, _b uint32) uint32 {
		return napiOK
	})
	exp("await_promise_sync", func(_ context.Context, m api.Module, _a, _b, _c uint32) {})
	exp("__getrandom_v03_custom", func(_ context.Context, m api.Module, ptr, length uint32) uint32 {
		e.mem = m.Memory()
		buf := make([]byte, length)
		_, _ = rand.Read(buf)
		_ = e.mem.Write(ptr, buf)
		return 0
	})
	exp("_emnapi_worker_ref", func(_ context.Context, m api.Module, _id uint32) {})
	exp("_emnapi_runtime_keepalive_push", func() {})
	exp("_emnapi_unwind", func() {})
	exp("_emnapi_is_main_browser_thread", func() uint32 { return 0 })
	exp("_emnapi_get_now", func() float64 { return 0 })
	exp("_emnapi_is_main_runtime_thread", func() uint32 { return 1 })
	exp("_emnapi_async_worker", func(_ context.Context, m api.Module, _id uint32) uint32 { return 0 })

	_, err := b.Instantiate(context.Background())
	return err
}

func writeF64(mem api.Memory, ptr uint32, v float64) uint32 {
	u := api.EncodeF64(v)
	_ = mem.WriteUint64Le(ptr, u)
	return napiOK
}

func isBool(v goja.Value) bool { _, ok := v.Export().(bool); return ok }
func isNumber(v goja.Value) bool {
	_, ok := v.Export().(int64)
	if ok {
		return true
	}
	_, ok = v.Export().(float64)
	return ok
}
func isString(v goja.Value) bool { _, ok := v.Export().(string); return ok }
func isSymbol(v goja.Value) bool { return false }
func isFunc(v goja.Value) bool   { _, ok := goja.AssertFunction(v); return ok }
func isObject(v goja.Value) bool { _, ok := v.(*goja.Object); return ok }

func isTyped(v goja.Value) bool {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return false
	}
	switch v.Export().(type) {
	case []byte, goja.ArrayBuffer:
		return true
	default:
		return false
	}
}

func asBytes(v goja.Value) []byte {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	switch t := v.Export().(type) {
	case []byte:
		return t
	case goja.ArrayBuffer:
		return t.Bytes()
	case string:
		return []byte(t)
	default:
		return nil
	}
}
