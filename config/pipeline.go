package config

import "fmt"

// Corrector preprocesses source text before parsing, returning the cleaned
// source and any diagnostics produced during correction.
type Corrector func(source string) (cleaned string, diags []Diagnostic)

// Validator checks a parsed Config and returns diagnostics.
// target is nil for general validation, or a Go struct pointer for
// struct-aligned validation.
type Validator func(cfg *Config, target any) []Diagnostic

// Pipeline chains correctors and validators for pluggable error correction.
type Pipeline struct {
	correctors     []Corrector
	validators     []Validator
	maxCorrections int
}

// NewPipeline creates an empty pipeline.
func NewPipeline() *Pipeline {
	return &Pipeline{}
}

// WithCorrectors appends pre-parse correctors.
func (p *Pipeline) WithCorrectors(corrs ...Corrector) *Pipeline {
	p.correctors = append(p.correctors, corrs...)
	return p
}

// WithValidators appends post-parse validators.
func (p *Pipeline) WithValidators(valids ...Validator) *Pipeline {
	p.validators = append(p.validators, valids...)
	return p
}

// WithMaxCorrections sets the maximum number of corrector hits allowed.
// If exceeded, all corrector info-diagnostics are upgraded to error and a
// config_too_many_corrections diagnostic is appended. 0 means unlimited.
func (p *Pipeline) WithMaxCorrections(n int) *Pipeline {
	p.maxCorrections = n
	return p
}

// Correct runs the corrector chain and returns the cleaned source.
// If a corrector produces an error-severity diagnostic, the chain is broken.
// If maxCorrections > 0 and the hit count exceeds it, all corrector info
// diagnostics are upgraded to error severity.
func (p *Pipeline) Correct(source string) (string, []Diagnostic) {
	var allDiags []Diagnostic
	for _, c := range p.correctors {
		cleaned, diags := c(source)
		allDiags = append(allDiags, diags...)
		source = cleaned
		// Break on error-severity diagnostic (e.g. FormatGuard rejection)
		for _, d := range diags {
			if d.Severity == "error" {
				return source, allDiags
			}
		}
	}
	if p.maxCorrections > 0 && len(allDiags) > p.maxCorrections {
		for i := range allDiags {
			if allDiags[i].Severity == "info" {
				allDiags[i].Severity = "error"
			}
		}
		allDiags = append(allDiags, Diagnostic{
			Code:     "config_too_many_corrections",
			Category: "correct",
			Severity: "error",
			Message:  "too many corrections applied, input may be too far from valid syntax",
			Hint:     "rewrite the config in Spore config or JSON syntax and retry",
			Actual:   fmt.Sprintf("%d corrections applied (max %d)", len(allDiags), p.maxCorrections),
		})
	}
	return source, allDiags
}

// Validate runs the validator chain on a parsed config.
func (p *Pipeline) Validate(cfg *Config, target any) []Diagnostic {
	var allDiags []Diagnostic
	for _, v := range p.validators {
		allDiags = append(allDiags, v(cfg, target)...)
	}
	return allDiags
}

// ParseWithPipeline applies the corrector chain, parses the config, then
// applies the validator chain. Returns the parsed config, all diagnostics
// (from both correction and validation), and any parse error.
func ParseWithPipeline(source string, pipe *Pipeline) (*Config, []Diagnostic, error) {
	if pipe == nil {
		pipe = NewPipeline()
	}

	cleaned, corDiags := pipe.Correct(source)
	cfg, err := Parse(cleaned)
	if err != nil {
		// Surface the parse diagnostics alongside the correction diagnostics so
		// a caller only needs the returned slice to see everything that is
		// wrong with the input.
		return nil, append(corDiags, SyntaxDiagnostics(err)...), err
	}

	valDiags := pipe.Validate(cfg, nil)
	allDiags := append(corDiags, valDiags...)
	return cfg, allDiags, nil
}

// ParseAndValidate parses source with default LLM correctors, then validates
// against the target Go struct. Convenience entry point.
func ParseAndValidate(source string, target any, extra ...Validator) (*Config, []Diagnostic, error) {
	pipe := NewPipeline().WithCorrectors(DefaultCorrectors()...).WithMaxCorrections(3)
	pipe = pipe.WithValidators(
		ValidateNoUnknownFields(),
		ValidateRequiredFields(),
		ValidateTypes(),
	)
	pipe = pipe.WithValidators(extra...)

	cleaned, corDiags := pipe.Correct(source)
	cfg, err := Parse(cleaned)
	if err != nil {
		return nil, append(corDiags, SyntaxDiagnostics(err)...), err
	}

	valDiags := pipe.Validate(cfg, target)
	allDiags := append(corDiags, valDiags...)
	return cfg, allDiags, nil
}

// DefaultCorrectors returns the standard chain for handling common LLM
// generation artifacts (JSON format, markdown blocks, equals syntax).
func DefaultCorrectors() []Corrector {
	return []Corrector{
		FormatGuard(),
		StripMarkdownCodeBlock(),
		StripJSONBrackets(),
		StripJSONQuotes(),
		StripJSONTrailingCommas(),
		NormalizeEquals(),
	}
}
