package std_test

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std"
)

// TestMultiModule_JsonAndHash combines json.encode with hash.md5 in a script.
func TestMultiModule_JsonAndHash(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import encode from "json"
import md5 from "hash"
fun hashAndEncode(): string { return encode(md5("hello")) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("hashAndEncode", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	s, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
	if s != `"5d41402abc4b2a76b9719d911017c592"` {
		t.Fatalf("expected quoted md5 hex, got %s", s)
	}
}

// TestMultiModule_UrlAndBase64 chains url.encode and base64.encode.
func TestMultiModule_UrlAndBase64(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import encode as urlEncode from "url"
import encode as base64Encode from "base64"
fun chainEncode(): string { return base64Encode(urlEncode("hello world!")) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("chainEncode", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	s, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
	expected := base64.StdEncoding.EncodeToString([]byte(url.QueryEscape("hello world!")))
	if s != expected {
		t.Fatalf("expected %s, got %s", expected, s)
	}
}

// TestMultiModule_StrconvAndMath parses the result of a math calculation.
func TestMultiModule_StrconvAndMath(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import pow from "math"
import formatFloat from "strconv"
fun calcAndFormat(): string { return formatFloat(pow(2.0, 10.0)) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calcAndFormat", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "1024" {
		t.Fatalf("expected 1024, got %+v", outcome.Payload)
	}
}

// TestMultiModule_FourModules combines hex, hash, json, and strings.
func TestMultiModule_FourModules(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import sha256 from "hash"
import encode as hexEncode from "hex"
import encode as jsonEncode from "json"
import toUpper from "strings"
fun pipeline(): string {
    var hashResult: string = sha256("secret")
    var hexResult: string = hexEncode(hashResult)
    var jsonResult: string = jsonEncode(hexResult)
    return toUpper(jsonResult)
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("pipeline", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	s, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
	if !strings.HasPrefix(s, `"`) || !strings.HasSuffix(s, `"`) {
		t.Fatalf("expected quoted JSON string, got %s", s)
	}
}
