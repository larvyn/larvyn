package proc

import (
	"encoding/json"
	"strings"
)

// ExtractJSON returns the JSON document in a command's output, skipping any
// banner or warning printed before it, or nil when there is none.
func ExtractJSON(s string) json.RawMessage {
	start := strings.IndexAny(s, "[{")
	if start < 0 {
		return nil
	}
	s = strings.TrimSpace(s[start:])
	if !json.Valid([]byte(s)) {
		return nil
	}
	return json.RawMessage(s)
}
