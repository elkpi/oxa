package jev

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/elkpi/oxa-protocol/go/v3/ir"
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
// score level, even though both collapse to the same float64. A token is
// represented as ±digits × 10^exp with digits stripped of leading and
// trailing zeros, so magnitude checks work on the digit strings and their
// order of magnitude without ever materializing a power of ten (exponents
// saturate instead of expanding adversarially long powers into memory).

var jsonNumberPattern = regexp.MustCompile(
	`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`,
)

type decimalParts struct {
	negative bool
	digits   string
	exp      int64
}

// parseExponentSaturating parses the exponent literal with saturation at
// ±2^62: the sign of the overflow preserves whether the value is
// astronomically large or small, which is all the range checks need.
func parseExponentSaturating(literal string) int64 {
	const limit = int64(1) << 62
	negative := strings.HasPrefix(literal, "-")
	body := strings.TrimPrefix(strings.TrimPrefix(literal, "+"), "-")
	var value int64
	for _, r := range body {
		if r < '0' || r > '9' {
			return 0
		}
		digit := int64(r - '0')
		if value > limit/10 {
			value = limit
			break
		}
		value *= 10
		if value > limit-digit {
			value = limit
			break
		}
		value += digit
	}
	if negative {
		return -value
	}
	return value
}

func parseDecimal(token string) (decimalParts, bool) {
	if !jsonNumberPattern.MatchString(token) {
		return decimalParts{}, false
	}
	negative := strings.HasPrefix(token, "-")
	body := strings.TrimPrefix(token, "-")
	exponent := int64(0)
	if eIndex := strings.IndexAny(body, "eE"); eIndex >= 0 {
		exponent = parseExponentSaturating(body[eIndex+1:])
		body = body[:eIndex]
	}
	var intDigits, fracDigits string
	if dot := strings.IndexByte(body, '.'); dot >= 0 {
		intDigits, fracDigits = body[:dot], body[dot+1:]
	} else {
		intDigits, fracDigits = body, ""
	}
	exp := exponent - int64(len(fracDigits))
	digits := strings.TrimLeft(intDigits+fracDigits, "0")
	if digits == "" {
		return decimalParts{negative: negative, digits: "0", exp: 0}, true
	}
	trailing := len(digits) - len(strings.TrimRight(digits, "0"))
	return decimalParts{
		negative: negative,
		digits:   digits[:len(digits)-trailing],
		exp:      exp + int64(trailing),
	}, true
}

func decimalIsZero(parts decimalParts) bool {
	return parts.digits == "0"
}

// decimalEqualsZero reports whether a JSON number token equals exactly 0,
// accepting any decimal spelling such as `0.0` or `0e0`.
func decimalEqualsZero(a json.Number) bool {
	parts, ok := parseDecimal(string(a))
	return ok && decimalIsZero(parts)
}

// decimalEqualsOne reports whether a JSON number token equals exactly 1,
// accepting any decimal spelling such as `1.0` or `1e0`.
func decimalEqualsOne(a json.Number) bool {
	parts, ok := parseDecimal(string(a))
	if !ok || decimalIsZero(parts) || parts.negative {
		return false
	}
	return parts.exp == 0 && parts.digits == "1"
}

// decimalInUnitInterval reports whether a JSON number token is exactly within
// [0,1] in decimal terms (spec/17 §4.1 noul range).
func decimalInUnitInterval(a json.Number) bool {
	parts, ok := parseDecimal(string(a))
	if !ok {
		return false
	}
	if decimalIsZero(parts) {
		return true
	}
	if parts.negative {
		return false
	}
	// A positive nonzero value is within [0,1] exactly when it is 1 or a
	// decimal fraction below 1: digits × 10^exp ≤ 1 with digits ≥ 1.
	if parts.exp > 0 {
		return false
	}
	if parts.exp == 0 {
		return parts.digits == "1"
	}
	// exp < 0: value = digits × 10^exp ≤ 1 ⇔ digits ≤ 10^(-exp). The bound
	// only matters when its digit count can reach len(digits); a longer
	// bound means digits is certainly below it.
	if -parts.exp+1 > int64(len(parts.digits)) {
		return true
	}
	bound := "1" + strings.Repeat("0", int(-parts.exp))
	if len(parts.digits) < len(bound) {
		return true
	}
	return len(parts.digits) == len(bound) && parts.digits <= bound
}

// scoreTokenWithinLevels validates a score value against the discrete 1..N
// levels exactly: the token must be an integer whose exact value falls in
// range (spec/17 §4.1).
func scoreTokenWithinLevels(a json.Number, levels int) bool {
	parts, ok := parseDecimal(string(a))
	if !ok || decimalIsZero(parts) || parts.negative {
		return false
	}
	// A fractional value (negative exp after trailing-zero stripping) is not
	// an integer level; a positive exp of 2 or more is at least 100, beyond
	// the 1..10 level bound.
	if parts.exp < 0 || parts.exp > 2 {
		return false
	}
	valueDigits := parts.digits + strings.Repeat("0", int(parts.exp))
	levelDigits := strconv.Itoa(levels)
	if len(valueDigits) != len(levelDigits) {
		return len(valueDigits) < len(levelDigits)
	}
	return valueDigits <= levelDigits
}
