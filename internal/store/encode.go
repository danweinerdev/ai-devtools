package store

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Encode renders v deterministically: two-space indent, no HTML escaping, a
// trailing newline. Struct fields keep declaration order and map keys sort, so
// the same value always yields the same bytes and diffs stay minimal.
//
// The result is decoded strictly before it's returned, so aid can never write
// a file it would refuse to read back (a nil slice encoding as null, say).
func Encode(file string, v any, version int) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encoding %s: %w", file, err)
	}
	if err := Decode(file, b.Bytes(), newOf(v), version); err != nil {
		return nil, fmt.Errorf("aid bug: refusing to write a file it can't read back: %w", err)
	}
	return b.Bytes(), nil
}
