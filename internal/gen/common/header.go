package common

import "strings"

// WriteHeader prepends the optional comment header to a file buffer. A single
// trailing blank line separates the header from generated content so editors
// render the boundary clearly. An empty header writes nothing, so callers can
// pass Options.Header through unconditionally.
func WriteHeader(b *strings.Builder, header string) {
	if header == "" {
		return
	}
	b.WriteString(header)
	if !strings.HasSuffix(header, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n")
}
