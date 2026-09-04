package napihost

import (
	"github.com/tetratelabs/wazero/api"
)

// envTrampoline is a tiny wasm module that exports shared memory as "env.memory"
// and re-exports each imported host function. wazero host modules cannot export
// memory, so Oxide's env.memory import is satisfied here.
func envTrampoline(fns []api.FunctionDefinition, memMin, memMax uint32) []byte {
	type sig struct {
		params  []api.ValueType
		results []api.ValueType
	}
	var types []sig
	typeIdx := map[string]uint32{}
	key := func(s sig) string {
		return string(s.params) + ">" + string(s.results)
	}
	indexOf := func(params, results []api.ValueType) uint32 {
		s := sig{params: params, results: results}
		k := key(s)
		if i, ok := typeIdx[k]; ok {
			return i
		}
		i := uint32(len(types))
		types = append(types, s)
		typeIdx[k] = i
		return i
	}

	type imp struct {
		name    string
		typeIdx uint32
	}
	var imports []imp
	for _, fn := range fns {
		mod, name, ok := fn.Import()
		if !ok || mod != "env" {
			continue
		}
		imports = append(imports, imp{name: name, typeIdx: indexOf(fn.ParamTypes(), fn.ResultTypes())})
	}

	var buf []byte
	buf = append(buf, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)

	// type
	var typeSec []byte
	typeSec = appendLEB(typeSec, uint32(len(types)))
	for _, t := range types {
		typeSec = append(typeSec, 0x60)
		typeSec = appendLEB(typeSec, uint32(len(t.params)))
		typeSec = append(typeSec, t.params...)
		typeSec = appendLEB(typeSec, uint32(len(t.results)))
		typeSec = append(typeSec, t.results...)
	}
	buf = appendSection(buf, 1, typeSec)

	// import from "host"
	var impSec []byte
	impSec = appendLEB(impSec, uint32(len(imports)))
	for _, im := range imports {
		impSec = appendName(impSec, "host")
		impSec = appendName(impSec, im.name)
		impSec = append(impSec, 0x00) // func
		impSec = appendLEB(impSec, im.typeIdx)
	}
	buf = appendSection(buf, 2, impSec)

	// memory (shared)
	var memSec []byte
	memSec = appendLEB(memSec, 1)
	memSec = append(memSec, 0x03) // max + shared
	memSec = appendLEB(memSec, memMin)
	memSec = appendLEB(memSec, memMax)
	buf = appendSection(buf, 5, memSec)

	// export memory + each func
	var expSec []byte
	expSec = appendLEB(expSec, uint32(1+len(imports)))
	expSec = appendName(expSec, "memory")
	expSec = append(expSec, 0x02) // memory
	expSec = appendLEB(expSec, 0)
	for i, im := range imports {
		expSec = appendName(expSec, im.name)
		expSec = append(expSec, 0x00) // func
		expSec = appendLEB(expSec, uint32(i))
	}
	buf = appendSection(buf, 7, expSec)
	return buf
}

func appendSection(buf []byte, id byte, payload []byte) []byte {
	buf = append(buf, id)
	buf = appendLEB(buf, uint32(len(payload)))
	return append(buf, payload...)
}

func appendName(buf []byte, s string) []byte {
	buf = appendLEB(buf, uint32(len(s)))
	return append(buf, s...)
}

func appendLEB(buf []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			buf = append(buf, b|0x80)
			continue
		}
		return append(buf, b)
	}
}
