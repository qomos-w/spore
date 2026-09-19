package script

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// Package describes a validated multi-module SporeApp source package.
type Package struct {
	AppID        string
	Version      string
	EntryModule  string
	Modules      map[string]string
	SchemaRefs   []string
	AssetRefs    []string
	Dependencies map[string]string
	Hash         string
}

func (p Package) Validate() error {
	if p.AppID == "" {
		return fmt.Errorf("package app ID cannot be empty")
	}
	if p.Version == "" {
		return fmt.Errorf("package version cannot be empty")
	}
	if p.EntryModule == "" {
		return fmt.Errorf("package entry module cannot be empty")
	}
	if _, ok := p.Modules[p.EntryModule]; !ok {
		return fmt.Errorf("package entry module %q not found", p.EntryModule)
	}
	return nil
}

// ComputeHash returns a deterministic hash over package metadata and modules.
func (p Package) ComputeHash() string {
	h := sha256.New()
	h.Write([]byte(p.AppID + "\x00" + p.Version + "\x00" + p.EntryModule + "\x00"))
	keys := make([]string, 0, len(p.Modules))
	for key := range p.Modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		h.Write([]byte(key + "\x00" + p.Modules[key] + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
