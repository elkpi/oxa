package vectest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// CompareJSON implements the normative comparison rules of vectors/README.md
// (rules 1-3):
//
//  1. structural equality: object key order is irrelevant, arrays are ordered
//     and compare element-wise, strings compare by exact code-point sequence;
//  2. integers stay integers: numeric leaves compare numerically but with
//     type fidelity, expected 1 does not match actual 1.0 (spec/01 INV-7);
//  3. raw-JSON string leaves (tool_use.input, input_json_delta.partial_json,
//     face-side tool-argument strings) are JSON strings and therefore already
//     covered by exact string comparison; they are never structurally
//     compared.
//
// It returns nil when the two documents are structurally equal, and an error
// describing the first difference otherwise.
func CompareJSON(expected, actual []byte) error {
	e, err := decode(expected)
	if err != nil {
		return fmt.Errorf("expected side: %w", err)
	}
	a, err := decode(actual)
	if err != nil {
		return fmt.Errorf("actual side: %w", err)
	}
	if diff := equalJSON(e, a); diff != "" {
		return fmt.Errorf("structural mismatch at %s", diff)
	}
	return nil
}

func decode(doc []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// equalJSON returns "" when a and b are structurally equal, else a path
// describing the first difference.
func equalJSON(a, b any) string {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return fmt.Sprintf(": expected object, got %T", b)
		}
		for k, v := range av {
			w, ok := bv[k]
			if !ok {
				return fmt.Sprintf(".%s: missing in actual", k)
			}
			if d := equalJSON(v, w); d != "" {
				return "." + k + d
			}
		}
		for k := range bv {
			if _, ok := av[k]; !ok {
				return fmt.Sprintf(".%s: unexpected in actual", k)
			}
		}
		return ""
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return fmt.Sprintf(": expected array, got %T", b)
		}
		if len(av) != len(bv) {
			return fmt.Sprintf(": expected %d elements, got %d", len(av), len(bv))
		}
		for i := range av {
			if d := equalJSON(av[i], bv[i]); d != "" {
				return fmt.Sprintf("[%d]", i) + d
			}
		}
		return ""
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return fmt.Sprintf(": expected number, got %T", b)
		}
		return equalNumber(av, bv)
	case string:
		bv, ok := b.(string)
		if !ok {
			return fmt.Sprintf(": expected string, got %T", b)
		}
		if av != bv {
			return fmt.Sprintf(": string differs: expected %q, got %q", av, bv)
		}
		return ""
	case bool:
		bv, ok := b.(bool)
		if !ok {
			return fmt.Sprintf(": expected bool, got %T", b)
		}
		if av != bv {
			return fmt.Sprintf(": bool differs: expected %v, got %v", av, bv)
		}
		return ""
	case nil:
		if b != nil {
			return fmt.Sprintf(": expected null, got %T", b)
		}
		return ""
	default:
		return fmt.Sprintf(": unexpected type %T", a)
	}
}

// equalNumber compares two JSON numbers numerically but with type fidelity
// (rule 2): an integer representation (no fraction or exponent part yielding
// an integral value in integer form) does not equal a floating-point
// representation of the same value. 1 != 1.0, while 0.5 == 0.50.
func equalNumber(a, b json.Number) string {
	ai, aInt := new(big.Int).SetString(a.String(), 10)
	bi, bInt := new(big.Int).SetString(b.String(), 10)
	if aInt && bInt {
		if ai.Cmp(bi) != 0 {
			return fmt.Sprintf(": integer differs: expected %s, got %s", a, b)
		}
		return ""
	}
	if aInt != bInt {
		// One side is an integer literal, the other carries a fraction or
		// exponent: type fidelity fails when the values are numerically
		// equal; when they are not, they differ anyway.
		af, _ := new(big.Rat).SetString(a.String())
		bf, _ := new(big.Rat).SetString(b.String())
		if af.Cmp(bf) == 0 {
			return fmt.Sprintf(": number type fidelity: expected %s, got %s", a, b)
		}
		return fmt.Sprintf(": number differs: expected %s, got %s", a, b)
	}
	af, okA := new(big.Rat).SetString(a.String())
	bf, okB := new(big.Rat).SetString(b.String())
	if !okA || !okB {
		return fmt.Sprintf(": unparseable number: expected %s, got %s", a, b)
	}
	if af.Cmp(bf) != 0 {
		return fmt.Sprintf(": number differs: expected %s, got %s", a, b)
	}
	return ""
}

// CompareLosses implements comparison rule 4: expected and reported losses
// match as unordered sets keyed on (path, field, reason); detail is
// informational and not compared. Every expected loss must be reported and
// every reported loss must be expected.
func CompareLosses(expected []Loss, reported []Loss) error {
	type key struct{ path, field, reason string }
	count := func(list []Loss) map[key]int {
		m := make(map[key]int, len(list))
		for _, l := range list {
			m[key{l.Path, l.Field, l.Reason}]++
		}
		return m
	}
	exp, got := count(expected), count(reported)
	var problems []string
	for k, n := range exp {
		if got[k] < n {
			problems = append(problems, fmt.Sprintf(
				"expected loss not reported (or reported fewer times): path=%q field=%q reason=%q", k.path, k.field, k.reason))
		}
	}
	for k, n := range got {
		if exp[k] < n {
			problems = append(problems, fmt.Sprintf(
				"unexpected loss reported: path=%q field=%q reason=%q", k.path, k.field, k.reason))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// ConvertLosses adapts an arbitrary loss slice with the canonical loss shape
// (path, field, reason, detail) into harness losses. Faces using ir.Loss get
// this conversion for free via json round-trip.
func ConvertLosses(losses any) ([]Loss, error) {
	if losses == nil {
		return nil, nil
	}
	raw, err := json.Marshal(losses)
	if err != nil {
		return nil, err
	}
	var out []Loss
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
