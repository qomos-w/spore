package diagnostics_test

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qomos-w/spore/diagnostics"

	// Blank imports pull in every package that registers diagnostic codes so
	// their init() runs and the full registry is observable from this test.
	_ "github.com/qomos-w/spore/binding"
	_ "github.com/qomos-w/spore/config"
	_ "github.com/qomos-w/spore/internal/script/bytecode"
	_ "github.com/qomos-w/spore/internal/script/frontend"
	_ "github.com/qomos-w/spore/invoke"
	_ "github.com/qomos-w/spore/runtime"
	_ "github.com/qomos-w/spore/transport"
)

var updateRegistrySnapshot = flag.Bool(
	"update-registry-snapshot", false,
	"rewrite testdata/registered_codes.txt from the live registry",
)

const registrySnapshotPath = "testdata/registered_codes.txt"

// formatRegistrySnapshot renders the registry deterministically: one
// tab-separated line per code, sorted by code (RegisteredCodes is sorted).
func formatRegistrySnapshot() string {
	var buf bytes.Buffer
	for _, info := range diagnostics.RegisteredCodes() {
		fmt.Fprintf(&buf, "%s\t%s\t%s\t%s\n", info.Code, info.Category, info.Description, info.Hint)
	}
	return buf.String()
}

// TestDiagnosticRegistrySnapshot pins the exact set of registered diagnostic
// codes (and each code's category/description/hint). It makes the table-driven
// registration refactor provably behavior-preserving: the code set captured
// before the refactor must still match afterwards.
func TestDiagnosticRegistrySnapshot(t *testing.T) {
	got := formatRegistrySnapshot()

	if *updateRegistrySnapshot {
		if err := os.MkdirAll(filepath.Dir(registrySnapshotPath), 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		if err := os.WriteFile(registrySnapshotPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		t.Logf("wrote registry snapshot: %s", registrySnapshotPath)
		return
	}

	want, err := os.ReadFile(registrySnapshotPath)
	if err != nil {
		t.Fatalf("read snapshot %s: %v (regenerate with -update-registry-snapshot)", registrySnapshotPath, err)
	}
	if !bytes.Equal([]byte(got), want) {
		t.Fatalf("registered diagnostic codes drifted from snapshot:\n%s", diffRegistrySnapshot(string(want), got))
	}
}

// diffRegistrySnapshot reports lines present in one side but not the other.
func diffRegistrySnapshot(want, got string) string {
	wantSet := lineSet(want)
	gotSet := lineSet(got)
	var b strings.Builder
	for _, l := range splitNonEmpty(got) {
		if !wantSet[l] {
			fmt.Fprintf(&b, "+ %s\n", l)
		}
	}
	for _, l := range splitNonEmpty(want) {
		if !gotSet[l] {
			fmt.Fprintf(&b, "- %s\n", l)
		}
	}
	if b.Len() == 0 {
		return "(same code set, different ordering)"
	}
	return b.String()
}

func lineSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, l := range splitNonEmpty(s) {
		set[l] = true
	}
	return set
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
