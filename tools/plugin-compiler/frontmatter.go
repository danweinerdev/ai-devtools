package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// A deliberately small YAML subset: scalars, quoted strings, booleans, inline
// [a, b] lists and block "- item" lists. Anything richer is an authoring error.
// A real YAML parser would reject plain scalars containing ": ", which skill
// descriptions use freely and every harness accepts.

var (
	frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?(.*)$`)
	commentRe     = regexp.MustCompile(`\s+#.*$`)
	itemRe        = regexp.MustCompile(`^\s+-\s+(.*)$`)
	keyValueRe    = regexp.MustCompile(`^([A-Za-z][\w-]*):\s*(.*)$`)
)

// parseFrontmatter splits a markdown file into its frontmatter values (string,
// bool, or []any) and its body. Problems are appended to errs, prefixed by file.
func parseFrontmatter(text, file string, errs *[]string) (map[string]any, string) {
	m := frontmatterRe.FindStringSubmatch(text)
	if m == nil {
		*errs = append(*errs, file+": missing frontmatter")
		return map[string]any{}, text
	}
	data := map[string]any{}
	listKey := ""
	for _, raw := range strings.Split(m[1], "\n") {
		line := commentRe.ReplaceAllString(raw, "")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if item := itemRe.FindStringSubmatch(line); item != nil && listKey != "" {
			data[listKey] = append(data[listKey].([]any), scalar(item[1], file, errs))
			continue
		}
		kv := keyValueRe.FindStringSubmatch(line)
		if kv == nil {
			*errs = append(*errs, fmt.Sprintf("%s: unsupported frontmatter line: %s", file, raw))
			continue
		}
		key, value := kv[1], kv[2]
		switch {
		case value == "":
			data[key] = []any{}
			listKey = key
		case strings.HasPrefix(value, "["):
			data[key] = inlineList(value, file, errs)
			listKey = ""
		default:
			data[key] = scalar(value, file, errs)
			listKey = ""
		}
	}
	return data, m[2]
}

func scalar(v, file string, errs *[]string) any {
	v = strings.TrimSpace(v)
	switch {
	case v == "true":
		return true
	case v == "false":
		return false
	case len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`):
		var s string
		if err := json.Unmarshal([]byte(v), &s); err != nil {
			*errs = append(*errs, fmt.Sprintf("%s: bad quoted string %s: %v", file, v, err))
		}
		return s
	case len(v) >= 2 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'"):
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

func inlineList(v, file string, errs *[]string) []any {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(v, "["), "]"))
	out := []any{}
	if inner == "" {
		return out
	}
	for _, part := range strings.Split(inner, ",") {
		out = append(out, scalar(part, file, errs))
	}
	return out
}

// asList reads a list value, or a comma-separated string, as strings.
func asList(v any) []string {
	out := []string{}
	switch v := v.(type) {
	case []any:
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
	case string:
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// str is a frontmatter value as a string, or "" when it isn't one.
func str(v any) string {
	s, _ := v.(string)
	return s
}
