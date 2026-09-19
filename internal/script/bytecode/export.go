package bytecode

import (
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/invoke"
)

// Exported type aliases for cross-package access.
type (
	Compiler = compiler
	Chunk    = chunk
)

// NewCompiler creates a new bytecode compiler.
func NewCompiler() *Compiler { return newCompiler() }

// --- Compiler exported methods ---

// Compile compiles a program AST into a main chunk.
func (c *Compiler) Compile(prog *frontend.Program) (*Chunk, error) {
	return c.compile(prog)
}

// CompileModule compiles a linked module under a stable module path prefix.
func (c *Compiler) CompileModule(path string, prog *frontend.Program) (*Chunk, error) {
	return c.compileModule(path, prog)
}

// GetFunctions returns all compiled function chunks.
func (c *Compiler) GetFunctions() map[string]*Chunk {
	return c.getFunctions()
}

// RegisterFunctions registers compiled functions into the VM.
func (c *Compiler) RegisterFunctions(v *vm.VM, interp *Interpreter) {
	c.registerFunctions(v, interp)
}

// RegisterClasses registers compiled classes into the VM.
func (c *Compiler) RegisterClasses(v *vm.VM) {
	c.registerClasses(v)
}

// RegisterNativeCapability records a native capability namespace for compiler lowering.
func (c *Compiler) RegisterNativeCapability(desc invoke.CapabilityDesc) {
	c.registerNativeCapability(desc)
}

// RegisterImportedCallableAlias records a local alias for a linked script callable.
func (c *Compiler) RegisterImportedCallableAlias(localName, targetName string) {
	c.importedCallables[localName] = targetName
}

// RegisterImportedGlobalAlias records a local alias for a linked imported global.
func (c *Compiler) RegisterImportedGlobalAlias(localName, targetName string) {
	c.importedGlobals[localName] = targetName
}

// RegisterImportedGlobalType records the static type for a linked imported global.
func (c *Compiler) RegisterImportedGlobalType(localName, typeName string) {
	c.globalTypes[localName] = typeName
}

// RegisterImportedNativeValue records a local alias for a read-only native value.
func (c *Compiler) RegisterImportedNativeValue(localName string, value vm.Value) {
	c.nativeValues[localName] = value
	if _, exists := c.nativeValueSlots[localName]; !exists {
		c.nativeValueSlots[localName] = len(c.nativeValueSlots)
	}
}

// RegisterGlobalType records the static type for a global name.
func (c *Compiler) RegisterGlobalType(name, typeName string) {
	c.globalTypes[name] = typeName
}

// RegisterImportedNativeAlias records a local alias for a native callable target.
func (c *Compiler) RegisterImportedNativeAlias(localName, targetName string) {
	c.importedNatives[localName] = targetName
}

// RegisterStructs registers compiled structs into the VM.
func (c *Compiler) RegisterStructs(v *vm.VM) {
	c.registerStructs(v)
}

// RegisterEnums registers compiled enums into the VM.
func (c *Compiler) RegisterEnums(v *vm.VM) {
	c.registerEnums(v)
}

// RegisterImportedEnumAlias records a local alias for a linked enum type so
// `Alias.Member` resolves against the target enum's declared name.
func (c *Compiler) RegisterImportedEnumAlias(localName, enumName string) {
	c.importedEnums[localName] = enumName
}
