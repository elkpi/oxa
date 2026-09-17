package oxa_test

import (
	"testing"

	"github.com/elkpi/oxa/go"
)

func TestVersion(t *testing.T) {
	if oxa.Version != "1.0.1" {
		t.Errorf("oxa.Version = %q, want 1.0.1", oxa.Version)
	}
}
