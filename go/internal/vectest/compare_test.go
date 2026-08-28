package vectest

import (
	"testing"
)

func TestCompareJSON(t *testing.T) {
	cases := []struct {
		name     string
		expected string
		actual   string
		equal    bool
	}{
		{"key order irrelevant", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{"nested arrays ordered", `[1,[2,3]]`, `[1,[3,2]]`, false},
		{"strings exact", `{"t":"a\nb"}`, `{"t":"a\nb "}`, false},
		{"integers stay integers", `{"n":1}`, `{"n":1.0}`, false},
		{"floats equal numerically", `{"n":0.5}`, `{"n":0.50}`, true},
		{"float vs int unequal", `{"n":1.5}`, `{"n":2}`, false},
		{"missing key", `{"a":1}`, `{"a":1,"b":2}`, false},
		{"extra key", `{"a":1,"b":2}`, `{"a":1}`, false},
	}
	for _, c := range cases {
		err := CompareJSON([]byte(c.expected), []byte(c.actual))
		if c.equal && err != nil {
			t.Errorf("%s: unexpected mismatch: %v", c.name, err)
		}
		if !c.equal && err == nil {
			t.Errorf("%s: expected mismatch, got equality", c.name)
		}
	}
}

func TestCompareLossesAsUnorderedSet(t *testing.T) {
	expected := []Loss{
		{Path: "b", Field: "y", Reason: "unmapped-value", Detail: "d1"},
		{Path: "a", Field: "x", Reason: "unmapped-field", Detail: "d2"},
	}
	reordered := []Loss{
		{Path: "a", Field: "x", Reason: "unmapped-field", Detail: "ignored"},
		{Path: "b", Field: "y", Reason: "unmapped-value"},
	}
	if err := CompareLosses(expected, reordered); err != nil {
		t.Errorf("order and detail must not matter: %v", err)
	}
	missing := []Loss{{Path: "a", Field: "x", Reason: "unmapped-field"}}
	if err := CompareLosses(expected, missing); err == nil {
		t.Error("expected a missing-loss error")
	}
	extra := append(append([]Loss{}, reordered...), Loss{Path: "c", Field: "z", Reason: "degraded"})
	if err := CompareLosses(expected, extra); err == nil {
		t.Error("expected an unexpected-loss error")
	}
	if err := CompareLosses(nil, nil); err != nil {
		t.Errorf("empty loss lists must compare equal: %v", err)
	}
}
