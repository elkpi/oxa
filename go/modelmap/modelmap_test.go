package modelmap

import "testing"

func TestMap(t *testing.T) {
	var nilTable Table
	if got := nilTable.Map("m"); got != "m" {
		t.Fatalf("nil table must be identity, got %q", got)
	}
	table := Table{"a": "b"}
	if got := table.Map("a"); got != "b" {
		t.Fatalf("hit: got %q", got)
	}
	if got := table.Map("c"); got != "c" {
		t.Fatalf("miss must fall back to identity, got %q", got)
	}
}
