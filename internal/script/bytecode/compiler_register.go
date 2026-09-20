// compiler_register.go installs compiled functions, classes, structs, enums and type aliases into the VM registries.

package bytecode

import (
	"fmt"
	"github.com/qomos-w/spore/invoke"
	"strings"

	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// GetFunctions returns all compiled function chunks.
func (c *compiler) getFunctions() map[string]*chunk {
	return c.functions
}

// RegisterFunctions registers compiled functions into the VM's function registry.
func (c *compiler) registerFunctions(v *vm.VM, interp *Interpreter) {
	for name, chunk := range c.functions {
		if strings.HasPrefix(name, "lambda$") {
			// Lambda chunks are only callable through closure values;
			// they must not be invoked by name (captures would be unbound).
			continue
		}
		info := c.funcInfo[name]
		if info == nil {
			continue
		}

		capturedChunk := chunk
		capturedInterp := interp

		body := vm.NewBytecodeFunctionBody(func(v *vm.VM, args []vm.Value) vm.Value {
			result, err := capturedInterp.ExecuteFunction(capturedChunk, invoke.InvocationStageUnary, args)
			if err != nil {
				// The vm package invokes registered bodies through a value-only
				// seam, so there is no error return for a RuntimeError to
				// travel on. Panic with the structured error itself: the
				// bytecode boundary recovery (see doc.go and panic_recovery.go)
				// unwraps it back into the very same *RuntimeError, preserving
				// its code/path/stack instead of degrading it to a generic
				// vm_internal_panic. Panicking with err.Error() here would lose
				// the diagnostic identity.
				panic(err)
			}
			return result
		})

		params := make([]vm.ParameterDef, info.paramCount)
		for i := range params {
			params[i] = vm.NewParameterDef(fmt.Sprintf("param%d", i), vm.TypeInvalid)
		}

		v.FuncReg().RegisterFunction(vm.NewFunctionDef(name, params, vm.TypeInvalid, body))
	}
}

// RegisterClasses registers compiled classes into the VM.
func (c *compiler) registerClasses(v *vm.VM) {
	classReg := v.ClassReg()

	// Pass 1: Create classes without parent.
	classMap := make(map[string]*vm.Class)
	for className := range c.classes {
		class := vm.NewClass(0, className, nil)
		classMap[className] = class
	}

	// Pass 1.5: Resolve parent references and validate inheritance constraints.
	for className, info := range c.classes {
		if info.parent != "" {
			parentClass, ok := classMap[info.parent]
			if !ok {
				// Parent is not a class; check if it's an interface.
				if _, isIface := c.interfaces[info.parent]; isIface {
					// Move from parent to implements.
					info.implements = append(info.implements, info.parent)
					info.parent = ""
					c.classes[className] = info
				} else {
					c.addCompileError("parent_class_not_found", "bytecode/oop/inheritance", fmt.Sprintf("parent class %q not found for %q", info.parent, className))
					continue
				}
			} else {
				// Enforce: only open classes can be inherited.
				parentInfo := c.classes[info.parent]
				if !parentInfo.isOpen {
					c.addCompileError("inherit_non_open_class", "bytecode/oop/inheritance", fmt.Sprintf("class %q cannot inherit from non-open class %q", className, info.parent))
					continue
				}
				classMap[className].SetParent(parentClass)
			}
		}
	}

	// Pass 1.6: Validate override methods exist in parent class.
	for className, info := range c.classes {
		for _, method := range info.methods {
			if !method.IsOverride {
				continue
			}
			methodName := method.Name.Value
			if info.parent == "" {
				c.addCompileError("override_without_parent", "bytecode/oop/override", fmt.Sprintf("method %q in class %q is marked override but class has no parent", methodName, className))
				continue
			}
			parentInfo, ok := c.classes[info.parent]
			if !ok {
				continue // already reported in pass 1.5
			}
			found := false
			for _, parentMethod := range parentInfo.methods {
				if parentMethod.Name.Value == methodName {
					paramCount := len(method.Params)
					parentParamCount := len(parentMethod.Params)
					if parentParamCount != paramCount {
						c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parameter count %d does not match parent count %d", methodName, paramCount, parentParamCount), fmt.Sprintf("%d parameter(s)", parentParamCount), fmt.Sprintf("%d parameter(s)", paramCount))
					} else if !methodReturnsCompatible(method, parentMethod) {
						c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: return type %q does not match parent return type %q", methodName, funReturnType(method), funReturnType(parentMethod)), funReturnType(parentMethod), funReturnType(method))
					}
					if !parentMethod.IsOpen {
						c.addCompileError("override_parent_method_not_open", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parent method in class %q is not open", methodName, info.parent))
					}
					found = true
					break
				}
			}
			if !found {
				c.addCompileError("override_method_not_found", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: method not found in parent class %q", methodName, info.parent))
			}
		}
	}

	// Pass 1.7: Validate interface contracts for classes that implement interfaces.
	for className, info := range c.classes {
		for _, ifaceName := range info.implements {
			iface, ok := c.interfaces[ifaceName]
			if !ok {
				c.addCompileError("unknown_interface", "bytecode/oop/interface", fmt.Sprintf("class %q implements unknown interface %q", className, ifaceName))
				continue
			}
			// Check each interface method is provided by the class.
			for _, ifaceMethod := range iface.methods {
				found := false
				expectedParams := len(ifaceMethod.Params)
				for _, classMethod := range info.methods {
					if classMethod.Name.Value == ifaceMethod.Name.Value {
						found = true
						if params := len(classMethod.Params); params != expectedParams {
							c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but method %q has parameter count %d, expected %d", className, ifaceName, ifaceMethod.Name.Value, params, expectedParams), fmt.Sprintf("%d parameter(s)", expectedParams), fmt.Sprintf("%d parameter(s)", params))
						}
						break
					}
				}
				if !found {
					c.addCompileError("interface_method_missing", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but is missing method %q", className, ifaceName, ifaceMethod.Name.Value))
				}
			}
		}
	}

	// Pass 2: Add fields.
	for className, classInfo := range c.classes {
		class := classMap[className]
		for _, f := range classInfo.fields {
			class.AddField(f.name, vm.TypeInvalid)
		}
	}

	// Compute topological order: parents before children.
	order := c.topologicalClassOrder()

	// Pass 3: Compute offsets (parents first), add methods, register.
	for _, className := range order {
		classInfo := c.classes[className]
		class := classMap[className]
		class.ComputeFieldOffsets()

		for _, method := range classInfo.methods {
			methodKey := className + "." + method.Name.Value
			funcDef := v.FuncReg().GetFunction(methodKey)
			if funcDef != nil {
				capturedFunc := funcDef
				impl := func(v *vm.VM, receiver vm.Handle, args []vm.Value) vm.Value {
					allArgs := make([]vm.Value, len(args)+1)
					allArgs[0] = vm.EncodeHandle(receiver)
					copy(allArgs[1:], args)
					return capturedFunc.ExecuteBody(v, allArgs)
				}
				if method.IsOverride {
					class.OverrideMethod(method.Name.Value, impl)
				} else if method.IsOpen {
					class.AddOpenMethod(method.Name.Value, impl)
				} else {
					class.AddMethod(method.Name.Value, impl)
				}
			}
		}

		class.BuildVTable()
		classReg.RegisterClass(class)
	}

	// Register interfaces into VM and record class→interface mappings.
	ifaceReg := v.IfaceReg()
	for ifaceName, ifaceInfo := range c.interfaces {
		methods := make([]vm.InterfaceMethodSig, len(ifaceInfo.methods))
		for i, m := range ifaceInfo.methods {
			methods[i] = vm.NewInterfaceMethodSig(m.Name.Value, len(m.Params))
		}
		ifaceReg.RegisterInterface(vm.NewInterfaceDef(ifaceName, methods))
	}
	for className, classInfo := range c.classes {
		for _, ifaceName := range classInfo.implements {
			ifaceReg.RegisterImplementation(className, ifaceName)
		}
	}
}

// topologicalClassOrder returns class names in dependency order (parents before children).
func (c *compiler) topologicalClassOrder() []string {
	visited := make(map[string]bool)
	var order []string

	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		info, ok := c.classes[name]
		if !ok {
			return
		}
		if info.parent != "" {
			visit(info.parent)
		}
		order = append(order, name)
	}

	for name := range c.classes {
		visit(name)
	}
	return order
}

// RegisterStructs registers compiled structs into the VM.
func (c *compiler) registerStructs(v *vm.VM) {
	structReg := v.StructReg()
	// Pass 1: register all struct names so IDs are allocated.
	for structName := range c.structs {
		structReg.RegisterStruct(structName, nil)
	}
	// Pass 2: compute field TypeIDs and update definitions.
	for structName, structInfo := range c.structs {
		fields := make([]vm.FieldDef, len(structInfo.fields))
		for i, field := range structInfo.fields {
			tid := resolveTypeID(field.typeName, structReg, v.EnumReg())
			fields[i] = vm.NewFieldDef(field.name, tid, i+1)
		}
		structReg.UpdateStructFields(structName, fields)
	}
}

// registerEnums registers compiled enums into the VM's enum registry so
// opEnumValue constants, `is`/`as` checks, and enum-typed struct fields can
// resolve the enum's runtime ID. Idempotent per enum name.
func (c *compiler) registerEnums(v *vm.VM) {
	enumReg := v.EnumReg()
	for _, info := range c.enums {
		members := make([]vm.EnumMemberDef, len(info.members))
		for i, m := range info.members {
			members[i] = vm.EnumMemberDef{Name: m.name, Value: m.value}
		}
		enumReg.RegisterEnum(info.name, members)
	}
}

// resolveTypeID maps a script type name string to a VM TypeID.
// It handles basic types, registered structs, registered enums, and array<T> generics.
func resolveTypeID(typeName string, structReg *vm.StructRegistry, enumReg *vm.EnumRegistry) vm.TypeID {
	switch typeName {
	case "bool":
		return vm.TypeBool
	case "byte":
		return vm.TypeByte
	case "short":
		return vm.TypeShort
	case "ushort":
		return vm.TypeUShort
	case "int":
		return vm.TypeInt
	case "uint":
		return vm.TypeUInt
	case "long":
		return vm.TypeLong
	case "ulong":
		return vm.TypeULong
	case "float":
		return vm.TypeFloat
	case "double":
		return vm.TypeDouble
	case "string":
		return vm.TypeString
	case "bytes":
		return vm.TypeBytes
	case "object":
		return vm.TypeObject
	case "any":
		return vm.TypeAny
	case "media":
		// Media values travel as {mime, src} reference maps; the VM carries
		// them untyped and the binding boundary enforces the value contract.
		return vm.TypeAny
	}
	if eid := enumReg.GetEnumID(typeName); eid != 0 {
		return vm.MakeEnumType(eid)
	}
	if sid := structReg.GetStructID(typeName); sid != 0 {
		return vm.TypeID(uint64(3)<<60 | uint64(sid))
	}
	if isFunTypeName(typeName) {
		// Function-typed fields hold first-class closure values (any).
		return vm.TypeAny
	}
	if strings.HasPrefix(typeName, "array<") && strings.HasSuffix(typeName, ">") {
		elemTypeName := typeName[6 : len(typeName)-1]
		elemTID := resolveTypeID(elemTypeName, structReg, enumReg)
		elemCategory := uint64((elemTID >> 60) & 0xF)
		elemData := uint64(elemTID) & 0x0FFFFFFFFFFFFFFF
		return vm.TypeID(uint64(1)<<60 | elemCategory<<56 | elemData)
	}
	return vm.TypeInvalid
}

// --- Type alias ---

// registerTypeAlias records a type alias mapping.
func (c *compiler) registerTypeAlias(stmt *frontend.TypeAliasStmt) {
	if stmt == nil || stmt.Alias == nil {
		return
	}
	c.typeAliases[stmt.Name.Value] = typeAnnotationName(stmt.Alias)
}

func expandTypeAliases(typeName string, aliases map[string]string, seen map[string]bool) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return ""
	}
	if resolved, ok := aliases[typeName]; ok {
		if seen[typeName] {
			return typeName
		}
		seen[typeName] = true
		return expandTypeAliases(resolved, aliases, seen)
	}
	if isFunTypeName(typeName) {
		if paramStr, retStr, ok := splitFunTypeName(typeName); ok {
			parts := splitTopLevelTypeArgs(paramStr)
			for i, part := range parts {
				parts[i] = expandTypeAliases(part, aliases, seen)
			}
			ret := expandTypeAliases(retStr, aliases, seen)
			return fmt.Sprintf("fun(%s):%s", strings.Join(parts, ","), ret)
		}
		return typeName
	}
	start := strings.Index(typeName, "<")
	end := strings.LastIndex(typeName, ">")
	if start < 0 || end <= start {
		return typeName
	}
	base := strings.TrimSpace(typeName[:start])
	inner := typeName[start+1 : end]
	parts := splitTopLevelTypeArgs(inner)
	for i, part := range parts {
		parts[i] = expandTypeAliases(part, aliases, seen)
	}
	if resolvedBase, ok := aliases[base]; ok {
		base = expandTypeAliases(resolvedBase, aliases, seen)
	}
	return fmt.Sprintf("%s<%s>", base, strings.Join(parts, ","))
}

func splitTopLevelTypeArgs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := make([]string, 0, 2)
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '<', '(':
			depth++
		case '>', ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

// resolveType resolves a type name through aliases to its base type.
func (c *compiler) resolveType(typeName string) string {
	return expandTypeAliases(typeName, c.typeAliases, make(map[string]bool))
}
