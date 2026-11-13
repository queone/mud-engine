package engine

import "testing"

func TestDeriveSeedMatchesPinnedVector(t *testing.T) {
	s := DeriveSeed(1, 2, 3)
	if s.A != 11755217590148075554 || s.B != 5545439021822549569 {
		t.Fatalf("DeriveSeed(1,2,3) = %d, %d; want 11755217590148075554, 5545439021822549569", s.A, s.B)
	}
	if s.Derivation() != DerivationSHA {
		t.Fatalf("derivation = %q, want %q", s.Derivation(), DerivationSHA)
	}
}

func TestDeriveSeedChangesWithEachInput(t *testing.T) {
	base := DeriveSeed(1, 2, 3)
	for name, s := range map[string]Seed{"world": DeriveSeed(2, 2, 3), "season": DeriveSeed(1, 3, 3), "game": DeriveSeed(1, 2, 4)} {
		if s.A == base.A && s.B == base.B {
			t.Errorf("changing the %s left the seed unchanged", name)
		}
	}
}

func TestRawAndZeroSeedsReportRawDerivation(t *testing.T) {
	if d := RawSeed(1, 2).Derivation(); d != "raw" {
		t.Errorf("RawSeed derivation = %q, want raw", d)
	}
	if d := (Seed{}).Derivation(); d != "raw" {
		t.Errorf("zero Seed derivation = %q, want raw", d)
	}
}
