package data

import "testing"

func TestIsValidBuffKind(t *testing.T) {
	for _, k := range AllBuffKinds {
		if !IsValidBuffKind(k) {
			t.Errorf("AllBuffKinds member %q rejected by IsValidBuffKind", k)
		}
	}
	if IsValidBuffKind("frobnicate") {
		t.Error("IsValidBuffKind accepted bogus kind")
	}
}

func TestIsValidPersistence(t *testing.T) {
	for _, p := range AllPersistence {
		if !IsValidPersistence(p) {
			t.Errorf("AllPersistence member %q rejected by IsValidPersistence", p)
		}
	}
	if IsValidPersistence("forever") {
		t.Error("IsValidPersistence accepted bogus value")
	}
}

func TestIsValidElement(t *testing.T) {
	if !IsValidElement("holy") || !IsValidElement("neutral") {
		t.Error("valid element rejected")
	}
	if IsValidElement("plasma") {
		t.Error("IsValidElement accepted bogus element")
	}
}

func TestBuffKindDebuffIsValid(t *testing.T) {
	if !IsValidBuffKind(BuffKindDebuff) {
		t.Fatalf("BuffKindDebuff (%q) not accepted by IsValidBuffKind", BuffKindDebuff)
	}
	if BuffKindDebuff != "debuff" {
		t.Fatalf("BuffKindDebuff = %q, want \"debuff\"", BuffKindDebuff)
	}
}

func TestBuffKindLandIsValid(t *testing.T) {
	if !IsValidBuffKind(BuffKindLand) {
		t.Fatalf("BuffKindLand (%q) not accepted by IsValidBuffKind", BuffKindLand)
	}
	if BuffKindLand != "land" {
		t.Fatalf("BuffKindLand = %q, want \"land\"", BuffKindLand)
	}
}
