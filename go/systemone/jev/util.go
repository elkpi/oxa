package jev

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
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

// --- exact decimal comparison ----------------------------------------------
// Range and integer validation must not round JSON number tokens through
// IEEE 754 doubles: 1.0000000000000001 is greater than 1 and 1.5 is not a
// score level, even though both collapse to the same float64.

var jsonNumberPattern = regexp.MustCompile(
	`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`,
)

var zeroDigitsPattern = regexp.MustCompile(`^0*$`)

type decimalParts struct {
	negative   bool
	intDigits  string
	fracDigits string
}

func parseDecimal(token string) (decimalParts, bool) {
	if !jsonNumberPattern.MatchString(token) {
		return decimalParts{}, false
	}
	negative := strings.HasPrefix(token, "-")
	body := strings.TrimPrefix(token, "-")
	exponent := 0
	if eIndex := strings.IndexAny(body, "eE"); eIndex >= 0 {
		parsed, err := strconv.Atoi(body[eIndex+1:])
		if err != nil {
			return decimalParts{}, false
		}
		exponent = parsed
		body = body[:eIndex]
	}
	var intDigits, fracDigits string
	if dot := strings.IndexByte(body, '.'); dot >= 0 {
		intDigits, fracDigits = body[:dot], body[dot+1:]
	} else {
		intDigits, fracDigits = body, ""
	}
	if exponent > 0 {
		if exponent >= len(fracDigits) {
			intDigits += fracDigits + strings.Repeat("0", exponent-len(fracDigits))
			fracDigits = ""
		} else {
			intDigits += fracDigits[:exponent]
			fracDigits = fracDigits[exponent:]
		}
	} else if exponent < 0 {
		shift := -exponent
		if shift >= len(intDigits) {
			fracDigits = strings.Repeat("0", shift-len(intDigits)) + intDigits + fracDigits
			intDigits = "0"
		} else {
			fracDigits = intDigits[len(intDigits)-shift:] + fracDigits
			intDigits = intDigits[:len(intDigits)-shift]
		}
	}
	intDigits = strings.TrimLeft(intDigits, "0")
	if intDigits == "" {
		intDigits = "0"
	}
	return decimalParts{negative: negative, intDigits: intDigits, fracDigits: fracDigits}, true
}

func isZeroDecimal(parts decimalParts) bool {
	return zeroDigitsPattern.MatchString(parts.intDigits) &&
		zeroDigitsPattern.MatchString(parts.fracDigits)
}

func compareMagnitude(a, b decimalParts) int {
	if len(a.intDigits) != len(b.intDigits) {
		if len(a.intDigits) < len(b.intDigits) {
			return -1
		}
		return 1
	}
	if a.intDigits != b.intDigits {
		if a.intDigits < b.intDigits {
			return -1
		}
		return 1
	}
	width := len(a.fracDigits)
	if len(b.fracDigits) > width {
		width = len(b.fracDigits)
	}
	af := a.fracDigits + strings.Repeat("0", width-len(a.fracDigits))
	bf := b.fracDigits + strings.Repeat("0", width-len(b.fracDigits))
	if af == bf {
		return 0
	}
	if af < bf {
		return -1
	}
	return 1
}

// compareDecimal compares a JSON number token against a decimal literal
// exactly; ok is false when the leaf is not a JSON number token shape.
func compareDecimal(a json.Number, b string) (int, bool) {
	left, ok := parseDecimal(string(a))
	if !ok {
		return 0, false
	}
	right, ok := parseDecimal(b)
	if !ok {
		return 0, false
	}
	leftZero := isZeroDecimal(left)
	rightZero := isZeroDecimal(right)
	if leftZero && rightZero {
		return 0, true
	}
	if leftZero {
		if right.negative {
			return 1, true
		}
		return -1, true
	}
	if rightZero {
		if left.negative {
			return -1, true
		}
		return 1, true
	}
	if left.negative != right.negative {
		if left.negative {
			return -1, true
		}
		return 1, true
	}
	magnitude := compareMagnitude(left, right)
	if left.negative {
		return -magnitude, true
	}
	return magnitude, true
}

// decimalInUnitInterval reports whether a JSON number token is exactly within
// [0,1] in decimal terms (spec/17 §4.1 noul range).
func decimalInUnitInterval(a json.Number) bool {
	low, ok := compareDecimal(a, "0")
	if !ok || low < 0 {
		return false
	}
	high, ok := compareDecimal(a, "1")
	if !ok || high > 0 {
		return false
	}
	return true
}

// scoreTokenWithinLevels validates a score value against the discrete 1..N
// levels exactly: the token must be an integer and its exact value must fall
// in range (spec/17 §4.1).
func scoreTokenWithinLevels(a json.Number, levels int) bool {
	parts, ok := parseDecimal(string(a))
	if !ok || parts.negative {
		return false
	}
	if !zeroDigitsPattern.MatchString(parts.fracDigits) {
		return false
	}
	digits := strings.TrimLeft(parts.intDigits, "0")
	if digits == "" {
		digits = "0"
	}
	if len(digits) > len(strconv.Itoa(levels)) {
		return false
	}
	value, err := strconv.Atoi(digits)
	if err != nil {
		return false
	}
	return value >= 1 && value <= levels
}
