package jev

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// sortedKeys returns the map keys in ascending lexicographic order; the
// canonical order for question IDs, criteria names, and JSON canon (spec/17 §7).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func isJSONNull(raw []byte) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

func hasCriteria(raw []byte) bool {
	return len(bytes.TrimSpace(raw)) > 0 && !isJSONNull(raw)
}

// canonicalAnswers renders the answers map as one canonical JSON object:
// top-level keys sorted ascending, each answer compacted but carried
// verbatim (inner key order and number forms preserved), no HTML escaping
// (spec/17 §7.5).
func canonicalAnswers(answers map[string]json.RawMessage) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(answers); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// canonicalJSON renders JSON with sorted object keys, no whitespace, and
// numbers preserved verbatim (spec/17 §7.6 state canon).
func canonicalJSON(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	b, err := marshalCanonical(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalCanonical(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range sortedKeys(t) {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := marshalCanonical(t[k])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			ib, err := marshalCanonical(item)
			if err != nil {
				return nil, err
			}
			buf.Write(ib)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return json.Marshal(v)
	}
}
