// Package store reads and writes aid's JSON files: strict decoding that
// reports every problem at once, deterministic encoding, and crash-safe
// atomic writes. Locking lives alongside (lock.go); this file never trusts
// what it reads.
package store

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// DecodeError lists every problem found in one file. A file is either fully
// valid or rejected; nothing is partly applied.
type DecodeError struct {
	File     string
	Problems []string
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("%s is invalid:\n  - %s", e.File, strings.Join(e.Problems, "\n  - "))
}

// Decode strictly decodes raw into v, which must be a pointer to a struct with
// an int `version` field. It rejects, all in one error:
//
//   - malformed JSON (with line and column), trailing data after the value;
//   - a version other than want (checked first: another version is another schema);
//   - unknown keys (with a did-you-mean), duplicate keys, nulls anywhere;
//   - missing keys: a field without `omitempty` is required;
//   - values of the wrong type.
func Decode(file string, raw []byte, v any, want int) error {
	fail := func(problems ...string) error { return &DecodeError{File: file, Problems: problems} }

	root, err := parseTree(raw)
	if err != nil {
		return fail(err.Error())
	}
	obj, ok := root.(*object)
	if !ok {
		return fail("the top level must be a JSON object")
	}
	switch ver, ok := obj.values["version"]; {
	case !ok:
		return fail(fmt.Sprintf("missing key `version` (this aid reads version %d)", want))
	case !isInt(ver, want):
		return fail(fmt.Sprintf("`version` is %s; this aid reads version %d", describe(ver), want))
	}

	var problems []string
	check(root, reflect.TypeOf(v).Elem(), "", &problems)
	if len(problems) > 0 {
		return fail(problems...)
	}
	// The tree already matches the type, so this only fails on values a
	// TextUnmarshaler rejects (a malformed timestamp, say).
	if err := json.Unmarshal(raw, v); err != nil {
		return fail(describeJSONError(raw, err))
	}
	return nil
}

// object keeps key order so problems are reported in file order.
type object struct {
	keys   []string
	values map[string]any
}

// parseTree parses one JSON value into objects, []any, json.Number, string,
// bool and nil, rejecting duplicate keys and anything after the value.
func parseTree(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var dup string
	var walk func(path string) (any, error)
	walk = func(path string) (any, error) {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				o := &object{values: map[string]any{}}
				for dec.More() {
					keyTok, err := dec.Token()
					if err != nil {
						return nil, err
					}
					key := keyTok.(string)
					val, err := walk(join(path, key))
					if err != nil {
						return nil, err
					}
					if _, seen := o.values[key]; seen && dup == "" {
						dup = join(path, key)
					}
					if _, seen := o.values[key]; !seen {
						o.keys = append(o.keys, key)
					}
					o.values[key] = val
				}
				_, err := dec.Token() // '}'
				return o, err
			case '[':
				arr := []any{}
				for i := 0; dec.More(); i++ {
					val, err := walk(fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return nil, err
					}
					arr = append(arr, val)
				}
				_, err := dec.Token() // ']'
				return arr, err
			}
		}
		return tok, nil
	}
	root, err := walk("")
	if err == nil {
		if _, extra := dec.Token(); extra != io.EOF {
			err = errors.New("unexpected data after the top-level value")
		}
	}
	if err != nil {
		return nil, errors.New(describeJSONError(raw, err))
	}
	if dup != "" {
		return nil, fmt.Errorf("duplicate key `%s`", dup)
	}
	return root, nil
}

var textUnmarshaler = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()

// check compares a parsed node against the Go type it will decode into.
func check(node any, t reflect.Type, path string, problems *[]string) {
	at := path
	if at == "" {
		at = "top level"
	}
	add := func(format string, args ...any) {
		*problems = append(*problems, fmt.Sprintf("`%s`: ", at)+fmt.Sprintf(format, args...))
	}
	if node == nil {
		add("null is not allowed")
		return
	}
	if reflect.PointerTo(t).Implements(textUnmarshaler) {
		if _, ok := node.(string); !ok {
			add("must be a string, got %s", describe(node))
		}
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := node.(*object)
		if !ok {
			add("must be an object, got %s", describe(node))
			return
		}
		fields := jsonFields(t)
		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, key := range obj.keys {
			f, ok := fields[key]
			if !ok {
				msg := fmt.Sprintf("unknown key `%s`", key)
				if near := nearest(key, names); near != "" {
					msg += fmt.Sprintf(" (did you mean `%s`?)", near)
				}
				*problems = append(*problems, fmt.Sprintf("`%s`: %s", at, msg))
				continue
			}
			check(obj.values[key], f.typ, join(path, key), problems)
		}
		for _, name := range names {
			if _, present := obj.values[name]; !present && fields[name].required {
				add("missing required key `%s`", name)
			}
		}
	case reflect.Map:
		obj, ok := node.(*object)
		if !ok {
			add("must be an object, got %s", describe(node))
			return
		}
		for _, key := range obj.keys {
			check(obj.values[key], t.Elem(), join(path, key), problems)
		}
	case reflect.Slice:
		arr, ok := node.([]any)
		if !ok {
			add("must be a list, got %s", describe(node))
			return
		}
		for i, el := range arr {
			check(el, t.Elem(), fmt.Sprintf("%s[%d]", path, i), problems)
		}
	case reflect.String:
		if _, ok := node.(string); !ok {
			add("must be a string, got %s", describe(node))
		}
	case reflect.Bool:
		if _, ok := node.(bool); !ok {
			add("must be true or false, got %s", describe(node))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := node.(json.Number)
		if !ok {
			add("must be an integer, got %s", describe(node))
		} else if _, err := strconv.ParseInt(string(n), 10, t.Bits()); err != nil {
			add("must be an integer, got %s", n)
		}
	case reflect.Interface:
		// any value but null, already rejected above
	default:
		add("has no JSON form (aid bug: unsupported field kind %s)", t.Kind())
	}
}

type field struct {
	typ      reflect.Type
	required bool
}

// jsonFields maps a struct's JSON names to their types. A field is required
// unless its tag says omitempty or omitzero.
func jsonFields(t reflect.Type) map[string]field {
	out := map[string]field{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		opts = "," + opts + ","
		optional := strings.Contains(opts, ",omitempty,") || strings.Contains(opts, ",omitzero,")
		out[name] = field{typ: f.Type, required: !optional}
	}
	return out
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func isInt(node any, want int) bool {
	n, ok := node.(json.Number)
	return ok && string(n) == strconv.Itoa(want)
}

func describe(node any) string {
	switch n := node.(type) {
	case nil:
		return "null"
	case *object:
		return "an object"
	case []any:
		return "a list"
	case string:
		return strconv.Quote(n)
	case json.Number:
		return string(n)
	case bool:
		return strconv.FormatBool(n)
	}
	return fmt.Sprint(node)
}

// nearest is the closest candidate within a small edit distance, or "".
func nearest(key string, candidates []string) string {
	best, bestDist := "", len(key)/3+2
	for _, c := range candidates {
		if d := levenshtein(strings.ToLower(key), strings.ToLower(c)); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// describeJSONError adds the line and column of the offending byte when the
// error carries an offset; encoding/json's own messages name no location.
func describeJSONError(raw []byte, err error) string {
	var off int64
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		off = syn.Offset
	case errors.As(err, &typ):
		off = typ.Offset
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected end of file"
	}
	if off <= 0 || off > int64(len(raw)) {
		return err.Error()
	}
	prefix := raw[:off]
	line := 1 + bytes.Count(prefix, []byte("\n"))
	col := int(off)
	if nl := bytes.LastIndexByte(prefix, '\n'); nl >= 0 {
		col = int(off) - nl - 1
	}
	return fmt.Sprintf("line %d, column %d: %s", line, col, err.Error())
}
