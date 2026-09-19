package schema

import (
	"fmt"
	"reflect"
	"strings"
)

// CodeMediaInlineTooLarge is the diagnostic code for oversized inline media.
// Contract: public semantic contract — stable error code across binding and transport.
const CodeMediaInlineTooLarge = "media_inline_too_large"

// MediaInlineLimit caps the src of data: URLs (the whole URL string, not just
// the payload segment). References are the persistent form of media values;
// inline bytes are a port-level convenience bounded to protect codecs.
const MediaInlineLimit = 1 << 20 // 1 MiB

// Media is the canonical host-side value form of the media schema type: a
// two-field reference carrier. On the wire and across the VM boundary the
// value projects as exactly {mime, src}; media bytes never enter the wire
// format.
type Media struct {
	Mime string `json:"mime"`
	Src  string `json:"src"`
}

var mediaStructType = reflect.TypeOf(Media{})

// IsMediaStructType reports whether t is a struct type with exported Mime/Src
// string fields — the duck-typed carrier shape accepted wherever a media
// value is validated (codegen-generated Media types included).
// Contract: public semantic contract — carrier acceptance rule shared by
// binding and transport validation.
func IsMediaStructType(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	m, mOK := t.FieldByName("Mime")
	s, sOK := t.FieldByName("Src")
	return mOK && sOK && m.Type.Kind() == reflect.String && s.Type.Kind() == reflect.String
}

// MediaInlineTooLargeError reports a data: src above MediaInlineLimit.
type MediaInlineTooLargeError struct {
	Size  int
	Limit int
}

func (e *MediaInlineTooLargeError) Error() string {
	return fmt.Sprintf("inline media data: URL is %d bytes, exceeding the %d byte limit", e.Size, e.Limit)
}

// ValidateMediaValue checks any of the accepted carrier shapes of a media
// value: Media, string-keyed maps (map[string]any and reflect-produced maps),
// and pointers to either. It enforces the value contract: non-empty
// "type/subtype" mime, non-empty src with a data:/file:/https: scheme, and
// the data: inline size cap.
// Contract: public semantic contract — the single validation authority for
// media values, shared by binding argument/result validation and transport
// encode-time rejection.
func ValidateMediaValue(value any) error {
	mime, src, err := mediaValueFields(value)
	if err != nil {
		return err
	}
	if mime == "" {
		return fmt.Errorf("media value is missing the \"mime\" field")
	}
	if !strings.Contains(mime, "/") || strings.HasPrefix(mime, "/") || strings.HasSuffix(mime, "/") || strings.ContainsAny(mime, " \t\r\n") {
		return fmt.Errorf("media mime %q is not a valid type/subtype pair", mime)
	}
	if src == "" {
		return fmt.Errorf("media value is missing the \"src\" field")
	}
	switch {
	case strings.HasPrefix(src, "data:"):
		if len(src) > MediaInlineLimit {
			return &MediaInlineTooLargeError{Size: len(src), Limit: MediaInlineLimit}
		}
	case strings.HasPrefix(src, "file:"), strings.HasPrefix(src, "https:"):
		// Reference forms: spore validates shape only; resolution semantics
		// belong to the host.
	default:
		return fmt.Errorf("media src must be a data:, file:, or https: reference, got %q", truncateForError(src, 48))
	}
	return nil
}

func mediaValueFields(value any) (mime string, src string, err error) {
	switch v := value.(type) {
	case Media:
		return v.Mime, v.Src, nil
	case *Media:
		if v == nil {
			return "", "", fmt.Errorf("media value is nil")
		}
		return v.Mime, v.Src, nil
	}
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", "", fmt.Errorf("media value is nil")
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct && rv.Type() != mediaStructType {
		// Duck-typed carriers: any struct with exported Mime/Src string fields
		// (e.g. codegen-generated Media types) is an accepted value form.
		if IsMediaStructType(rv.Type()) {
			return rv.FieldByName("Mime").String(), rv.FieldByName("Src").String(), nil
		}
	}
	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		lookup := func(key string) (string, bool) {
			ev := rv.MapIndex(reflect.ValueOf(key).Convert(rv.Type().Key()))
			if !ev.IsValid() {
				return "", false
			}
			for ev.Kind() == reflect.Interface {
				if ev.IsNil() {
					return "", false
				}
				ev = ev.Elem()
			}
			if ev.Kind() != reflect.String {
				return "", false
			}
			return ev.String(), true
		}
		mime, hasMime := lookup("mime")
		src, hasSrc := lookup("src")
		if !hasMime || !hasSrc {
			return "", "", fmt.Errorf("media value must carry \"mime\" and \"src\" string fields")
		}
		return mime, src, nil
	}
	return "", "", fmt.Errorf("media value must be a {mime, src} map or schema.Media, got %T", value)
}

func truncateForError(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
