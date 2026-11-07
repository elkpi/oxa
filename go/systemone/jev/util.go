package jev

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"github.com/elkpi/oxa/go/v3/ir"
)

// orderedTopKeys returns the top-level object keys of raw in source
// encounter order (spec/02 §4: losses follow the order the source
// constructs are encountered).
func orderedTopKeys(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		if key, ok := t.(string); ok {
			keys = append(keys, key)
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}

// mergeSourceOrder interleaves the typed-decode losses (whose paths root at
// top-level keys) with the raw-audit losses by source position, keeping the
// original relative order within each group (spec/02 §4 encounter order).
func mergeSourceOrder(raw []byte, rawLosses, typedLosses []ir.Loss) []ir.Loss {
	if len(typedLosses) == 0 {
		return rawLosses
	}
	position := make(map[string]int)
	for i, k := range orderedTopKeys(raw) {
		if _, dup := position[k]; !dup {
			position[k] = i
		}
	}
	headOf := func(path string) string {
		if i := strings.IndexAny(path, ".["); i >= 0 {
			return path[:i]
		}
		return path
	}
	type entry struct {
		order int
		loss  ir.Loss
	}
	entries := make([]entry, 0, len(rawLosses)+len(typedLosses))
	for _, l := range rawLosses {
		entries = append(entries, entry{position[l.Path], l})
	}
	for _, l := range typedLosses {
		entries = append(entries, entry{position[headOf(l.Path)], l})
	}
	sort.SliceStable(entries, func(a, b int) bool {
		return entries[a].order < entries[b].order
	})
	out := make([]ir.Loss, len(entries))
	for i, e := range entries {
		out[i] = e.loss
	}
	return out
}

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
