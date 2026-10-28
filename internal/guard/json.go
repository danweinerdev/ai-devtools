package guard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Object is a JSON object that keeps its key order, so a hook's input can be
// echoed back (the model override's updatedInput) exactly as it came in. A
// repeated key keeps its first position and its last value.
type Object struct {
	Keys   []string
	Values map[string]any
}

// Get returns the value for key, or nil.
func (o *Object) Get(key string) any {
	if o == nil {
		return nil
	}
	return o.Values[key]
}

// Has reports whether key is present.
func (o *Object) Has(key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.Values[key]
	return ok
}

// Set replaces key's value, appending the key if it's new.
func (o *Object) Set(key string, value any) {
	if _, ok := o.Values[key]; !ok {
		o.Keys = append(o.Keys, key)
	}
	o.Values[key] = value
}

// MarshalJSON writes the object in key order.
func (o *Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			b.WriteString(", ")
		}
		key, _ := json.Marshal(k)
		b.Write(key)
		b.WriteString(": ")
		val, err := json.Marshal(o.Values[k])
		if err != nil {
			return nil, err
		}
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// parse reads one JSON value: *Object, []any, json.Number, string, bool or nil.
// Anything after the value is an error.
func parse(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var walk func() (any, error)
	walk = func() (any, error) {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return tok, nil
		}
		switch delim {
		case '{':
			o := &Object{Values: map[string]any{}}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := walk()
				if err != nil {
					return nil, err
				}
				o.Set(keyTok.(string), val)
			}
			_, err := dec.Token()
			return o, err
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := walk()
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			_, err := dec.Token()
			return arr, err
		}
		return nil, fmt.Errorf("unexpected %v", delim)
	}
	v, err := walk()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("unexpected end of input")
		}
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("extra data after the value")
	}
	return v, nil
}

// str is v when it's a string, else "".
func str(v any) string {
	s, _ := v.(string)
	return s
}

// truthy is false for null, false, 0, "", [] and {}, and true otherwise.
func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case json.Number:
		f, err := v.Float64()
		return err != nil || f != 0
	case []any:
		return len(v) > 0
	case *Object:
		return len(v.Keys) > 0
	}
	return true
}

// display renders a JSON value for a message: strings bare, the rest as JSON.
func display(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		return v
	case json.Number:
		return v.String()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// integer reads a JSON number as an int when it's written as one, with no
// fraction or exponent.
func integer(v any) (int, bool) {
	n, ok := v.(json.Number)
	if !ok || strings.ContainsAny(string(n), ".eE") {
		return 0, false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		if strings.HasPrefix(string(n), "-") {
			return 0, false
		}
		return int(^uint(0) >> 1), true // an enormous budget is still a budget
	}
	return int(i), true
}
