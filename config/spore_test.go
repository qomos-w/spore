package config

import (
	"strings"
	"testing"
)

func TestSporeSchema_ConfigTypes(t *testing.T) {
	cfg, err := Parse(`
struct Point {
    x: double
    y: double
}

struct Database {
    host: string
    port: int
}

origin: Point{x: 0.0, y: 0.0}
debug: false
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out := SporeSchema(cfg)
	if !strings.Contains(out, "struct Point {") {
		t.Errorf("missing struct Point: %q", out)
	}
	if !strings.Contains(out, "x: double") {
		t.Errorf("missing x: double: %q", out)
	}
	if !strings.Contains(out, "struct Database {") {
		t.Errorf("missing struct Database: %q", out)
	}
	if !strings.Contains(out, "var origin: Point") {
		t.Errorf("missing var origin: %q", out)
	}
	if !strings.Contains(out, "var debug: bool") {
		t.Errorf("missing var debug: %q", out)
	}
}

func TestSporeSchemaForStruct(t *testing.T) {
	type Database struct {
		Host     string
		Port     int
		Database string
	}
	out, err := SporeSchemaForStruct(Database{})
	if err != nil {
		t.Fatalf("SporeSchemaForStruct: %v", err)
	}
	if !strings.Contains(out, "struct Database {") {
		t.Errorf("missing struct header: %q", out)
	}
	if !strings.Contains(out, "Host: string") {
		t.Errorf("missing Host: string: %q", out)
	}
	if !strings.Contains(out, "Port: int") {
		t.Errorf("missing Port: int: %q", out)
	}
	if !strings.Contains(out, "Database: string") {
		t.Errorf("missing Database: string: %q", out)
	}
}

func TestSporeSchemaForStruct_NestedSlice(t *testing.T) {
	type Config struct {
		Items  []string
		Counts map[string]int
	}
	out, err := SporeSchemaForStruct(Config{})
	if err != nil {
		t.Fatalf("SporeSchemaForStruct: %v", err)
	}
	if !strings.Contains(out, "Items: array<string>") {
		t.Errorf("missing Items type: %q", out)
	}
	if !strings.Contains(out, "Counts: map<string, int>") {
		t.Errorf("missing Counts type: %q", out)
	}
}
