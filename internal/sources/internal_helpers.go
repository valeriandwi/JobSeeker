package sources

import "bytes"

func newBytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
