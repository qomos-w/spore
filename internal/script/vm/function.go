package vm

// functionBody is the interface for executable function bodies.
type functionBody interface {
	execute(v *vm, args []value) value
}

// nativeFunctionBody wraps a Go function as a functionBody.
type nativeFunctionBody struct {
	fn func(v *vm, args []value) value
}

func (nfb *nativeFunctionBody) execute(v *vm, args []value) value {
	return nfb.fn(v, args)
}

// bytecodeFunctionBody wraps a bytecode chunk execution as a functionBody.
// Uses a closure to avoid circular dependencies between bytecode and vm packages.
type bytecodeFunctionBody struct {
	executeFn func(v *vm, args []value) value
}

func (bfb *bytecodeFunctionBody) execute(v *vm, args []value) value {
	return bfb.executeFn(v, args)
}

// functionDef holds a function's metadata and body.
type functionDef struct {
	name       string
	parameters []paramDef
	returnType typeID
	body       functionBody
}

// paramDef describes a function parameter.
type paramDef struct {
	name   string
	typeID typeID
}

// functionRegistry maps function names to their definitions.
type functionRegistry struct {
	functions map[string]*functionDef
}

func newFunctionRegistry() *functionRegistry {
	return &functionRegistry{
		functions: make(map[string]*functionDef),
	}
}

func (fr *functionRegistry) register(fn *functionDef) {
	fr.functions[fn.name] = fn
}

func (fr *functionRegistry) registerNative(name string, params []paramDef, retType typeID, fn func(v *vm, args []value) value) {
	fr.register(&functionDef{
		name:       name,
		parameters: params,
		returnType: retType,
		body:       &nativeFunctionBody{fn: fn},
	})
}

func (fr *functionRegistry) get(name string) *functionDef {
	return fr.functions[name]
}

func (fr *functionRegistry) call(v *vm, name string, args []value) value {
	fn := fr.get(name)
	if fn == nil {
		return encodeInt(0)
	}
	if len(args) != len(fn.parameters) {
		return encodeInt(0)
	}
	return fn.body.execute(v, args)
}

func (fr *functionRegistry) has(name string) bool {
	return fr.functions[name] != nil
}
