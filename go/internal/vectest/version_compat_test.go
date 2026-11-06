package vectest

import "testing"

func TestCompareLegacyIRVersionsWithCurrent(t *testing.T) {
	for _, version := range []string{"0.1.0", "0.2.0"} {
		if !isCompatibleSpecVersion(version, "0.3.0") || !isCompatibleSpecVersion("0.3.0", version) {
			t.Errorf("expected %s and 0.3.0 to compare as compatible", version)
		}
	}
	if isCompatibleSpecVersion("9.0.0", "0.3.0") {
		t.Fatal("unknown version accepted")
	}
}
