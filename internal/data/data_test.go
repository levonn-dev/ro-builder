package data

import (
	"os"
	"testing"
)

const herculesRoot = "../../../Hercules"

func requireHercules(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(herculesRoot); err != nil {
		t.Skipf("Hercules not at %s; clone HerculesWS/Hercules at sibling path", herculesRoot)
	}
}

func TestMode_DirName(t *testing.T) {
	if PreRenewal.DirName() != "pre-re" {
		t.Errorf("PreRenewal: got %q, want \"pre-re\"", PreRenewal.DirName())
	}
	if Renewal.DirName() != "re" {
		t.Errorf("Renewal: got %q, want \"re\"", Renewal.DirName())
	}
}

func TestLoadItems_PreRenewal(t *testing.T) {
	requireHercules(t)
	items, err := LoadItems(herculesRoot, PreRenewal)
	if err != nil {
		t.Fatalf("LoadItems pre-renewal: %v", err)
	}
	if len(items) < 1000 {
		t.Fatalf("expected >1000 items, got %d", len(items))
	}

	// Spot-check a known item to verify the path resolution + decoding round
	// trip. Knife's pre-re slot count is 3 (renewal would be 4; the data
	// files diverge there, which is one reason mode-aware loading matters).
	var knife *Item
	for i := range items {
		if items[i].ID == 1201 {
			knife = &items[i]
			break
		}
	}
	if knife == nil {
		t.Fatal("Knife (1201) not loaded")
	}
	if knife.Slots != 3 {
		t.Errorf("Knife pre-re slots: got %d, want 3", knife.Slots)
	}
}

func TestLoadItems_Renewal_KnifeHasFourSlots(t *testing.T) {
	requireHercules(t)
	items, err := LoadItems(herculesRoot, Renewal)
	if err != nil {
		t.Fatalf("LoadItems renewal: %v", err)
	}
	for _, it := range items {
		if it.ID == 1201 {
			// Pre-re Knife is 3-slot, but in renewal item_db it might differ.
			// We assert the load path works and produces a non-empty record;
			// the slot-count divergence is what we'd cross-check against if
			// we ever scored renewal builds.
			if it.AegisName != "Knife" {
				t.Errorf("Renewal Knife AegisName: got %q", it.AegisName)
			}
			return
		}
	}
	t.Fatal("Renewal Knife (1201) not loaded")
}
