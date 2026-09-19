package config

import (
	"testing"
)

func TestDeserialize_Scalars(t *testing.T) {
	type Scalars struct {
		Int    int
		Float  float64
		String string
		Bool   bool
	}
	cfg, err := Parse(`
int: 42
float: 3.14
string: "hello"
bool: true
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s Scalars
	if err := Deserialize(cfg, &s); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if s.Int != 42 {
		t.Errorf("Int: expected 42, got %d", s.Int)
	}
	if s.Float != 3.14 {
		t.Errorf("Float: expected 3.14, got %v", s.Float)
	}
	if s.String != "hello" {
		t.Errorf("String: expected hello, got %q", s.String)
	}
	if !s.Bool {
		t.Errorf("Bool: expected true")
	}
}

func TestDeserialize_NestedStruct(t *testing.T) {
	type Pool struct {
		Min int
		Max int
	}
	type Database struct {
		Host string
		Port int
		Pool Pool
	}
	cfg, err := Parse(`
host: "localhost"
port: 5432
pool {
    min: 5
    max: 20
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var db Database
	if err := Deserialize(cfg, &db); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if db.Host != "localhost" {
		t.Errorf("Host: expected localhost, got %q", db.Host)
	}
	if db.Port != 5432 {
		t.Errorf("Port: expected 5432, got %d", db.Port)
	}
	if db.Pool.Min != 5 || db.Pool.Max != 20 {
		t.Errorf("Pool: expected {5 20}, got %+v", db.Pool)
	}
}

func TestDeserialize_Slice(t *testing.T) {
	type Config struct {
		Items []string
	}
	cfg, err := Parse(`items: ["alpha", "beta", "gamma"]`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var c Config
	if err := Deserialize(cfg, &c); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if len(c.Items) != 3 || c.Items[0] != "alpha" || c.Items[2] != "gamma" {
		t.Errorf("Items: expected [alpha beta gamma], got %v", c.Items)
	}
}

func TestDeserialize_IntSlice(t *testing.T) {
	type Config struct {
		Ports []int
	}
	cfg, err := Parse(`ports: [80, 443, 8080]`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var c Config
	if err := Deserialize(cfg, &c); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if len(c.Ports) != 3 || c.Ports[1] != 443 {
		t.Errorf("Ports: expected [80 443 8080], got %v", c.Ports)
	}
}

func TestDeserialize_Map(t *testing.T) {
	type Config struct {
		Labels map[string]string
	}
	cfg, err := Parse(`labels: {env: "prod", team: "core"}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var c Config
	if err := Deserialize(cfg, &c); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if c.Labels["env"] != "prod" || c.Labels["team"] != "core" {
		t.Errorf("Labels: expected env=prod team=core, got %v", c.Labels)
	}
}

func TestDeserialize_MissingField(t *testing.T) {
	type Config struct {
		X int
		Y int
	}
	cfg, err := Parse(`x: 42`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var c Config
	if err := Deserialize(cfg, &c); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if c.X != 42 {
		t.Errorf("X: expected 42, got %d", c.X)
	}
	if c.Y != 0 {
		t.Errorf("Y: expected 0 (zero value), got %d", c.Y)
	}
}

func TestDeserialize_StructLiteral(t *testing.T) {
	type Point struct {
		X float64
		Y float64
	}
	type Config struct {
		Origin Point
	}
	cfg, err := Parse(`origin: Point{x: 1.0, y: 2.0}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var c Config
	if err := Deserialize(cfg, &c); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if c.Origin.X != 1.0 || c.Origin.Y != 2.0 {
		t.Errorf("Origin: expected {1 2}, got %+v", c.Origin)
	}
}

func TestDeserialize_UintField(t *testing.T) {
	type Config struct {
		Port uint
	}
	cfg, err := Parse(`port: 8080`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var c Config
	if err := Deserialize(cfg, &c); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if c.Port != 8080 {
		t.Errorf("Port: expected 8080, got %d", c.Port)
	}
}

func TestDeserialize_NilPointer(t *testing.T) {
	err := Deserialize(&Config{}, nil)
	if err == nil {
		t.Fatal("expected error for nil target")
	}
}

func TestDeserialize_NonPointer(t *testing.T) {
	var s struct{ X int }
	err := Deserialize(&Config{}, s)
	if err == nil {
		t.Fatal("expected error for non-pointer target")
	}
}
