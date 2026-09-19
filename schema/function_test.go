package schema

import (
	"errors"
	"testing"
)

func greet(id int, name string) string {
	return name
}

func touch(id int) {}

func fail(id int) error {
	return errors.New("boom")
}

func load(id int) (string, error) {
	return "", nil
}

func pair() (int, string) {
	return 0, ""
}

func variadic(args ...int) {}

func TestDescribeGoFunction_ProjectsParametersAndReturn(t *testing.T) {
	desc, err := DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction returned error: %v", err)
	}
	if desc.Name != "greet" {
		t.Fatalf("expected callable name greet, got %s", desc.Name)
	}
	if len(desc.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(desc.Parameters))
	}
	if desc.Parameters[0].Name != "arg0" || desc.Parameters[0].Type.Name != "int" {
		t.Fatalf("unexpected first parameter: %#v", desc.Parameters[0])
	}
	if desc.Parameters[1].Name != "arg1" || desc.Parameters[1].Type.Name != "string" {
		t.Fatalf("unexpected second parameter: %#v", desc.Parameters[1])
	}
	if len(desc.Returns) != 1 {
		t.Fatalf("expected 1 return type, got %d", len(desc.Returns))
	}
	if desc.Returns[0].Name != "string" {
		t.Fatalf("expected string return type, got %#v", desc.Returns[0])
	}
	if desc.HasError {
		t.Fatal("expected HasError false")
	}
}

func TestDescribeGoFunction_SupportsNoReturn(t *testing.T) {
	desc, err := DescribeGoFunction("touch", touch)
	if err != nil {
		t.Fatalf("DescribeGoFunction returned error: %v", err)
	}
	if len(desc.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(desc.Parameters))
	}
	if len(desc.Returns) != 0 {
		t.Fatalf("expected no return types, got %d", len(desc.Returns))
	}
	if desc.HasError {
		t.Fatal("expected HasError false")
	}
}

func TestDescribeGoFunction_SupportsTrailingError(t *testing.T) {
	errorOnly, err := DescribeGoFunction("fail", fail)
	if err != nil {
		t.Fatalf("DescribeGoFunction returned error: %v", err)
	}
	if len(errorOnly.Returns) != 0 {
		t.Fatalf("expected no non-error returns, got %d", len(errorOnly.Returns))
	}
	if !errorOnly.HasError {
		t.Fatal("expected HasError true")
	}

	withValue, err := DescribeGoFunction("load", load)
	if err != nil {
		t.Fatalf("DescribeGoFunction returned error: %v", err)
	}
	if len(withValue.Returns) != 1 {
		t.Fatalf("expected 1 non-error return, got %d", len(withValue.Returns))
	}
	if withValue.Returns[0].Name != "string" {
		t.Fatalf("expected string return type, got %#v", withValue.Returns[0])
	}
	if !withValue.HasError {
		t.Fatal("expected HasError true")
	}
}

func TestDescribeGoFunction_RejectsMultipleNonErrorReturns(t *testing.T) {
	_, err := DescribeGoFunction("pair", pair)
	if err == nil {
		t.Fatal("expected error for multiple non-error returns")
	}
}

func TestDescribeGoFunction_RejectsVariadic(t *testing.T) {
	_, err := DescribeGoFunction("variadic", variadic)
	if err == nil {
		t.Fatal("expected error for variadic function")
	}
}

func TestDescribeGoFunction_RejectsNonFunction(t *testing.T) {
	_, err := DescribeGoFunction("bad", 123)
	if err == nil {
		t.Fatal("expected error for non-function")
	}
}

func badParam(ch chan int) {}

func TestDescribeGoFunction_RejectsUnsupportedParamType(t *testing.T) {
	_, err := DescribeGoFunction("badParam", badParam)
	if err == nil {
		t.Fatal("expected error for unsupported parameter type")
	}
}

func badReturn() chan int { return nil }

func TestDescribeGoFunction_RejectsUnsupportedReturnType(t *testing.T) {
	_, err := DescribeGoFunction("badReturn", badReturn)
	if err == nil {
		t.Fatal("expected error for unsupported return type")
	}
}
