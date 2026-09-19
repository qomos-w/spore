package config

// PipelineAST represents a parsed pipeline declaration.
type PipelineAST struct {
	Name  string
	Steps []PipelineStepAST
	Line  int
}

// PipelineStepAST represents a single step within a pipeline.
type PipelineStepAST struct {
	Name      string
	Invoke    string
	Input     Value
	DependsOn []string
	Timeout   string
	When      *RefExpr
	Parallel  []PipelineStepAST
	Line      int
}

// RefExpr represents a step reference expression (e.g. generate.output).
// In the config layer it is only syntax-validated, not evaluated.
type RefExpr struct {
	StepName string
	Path     []string
	Raw      string
	Line     int
}
