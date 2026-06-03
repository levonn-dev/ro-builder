package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/data"
)

func TestLoadSkillBuffs_ValidatesKind(t *testing.T) {
	entry := skillBuffEntry{AegisName: "TK_RUN", SelfBuff: &data.SelfBuff{Name: "x", Kind: "bogus", Persistence: "transient"}}
	if err := validateSkillBuffEntry(0, entry); err == nil {
		t.Fatal("expected error for bogus kind, got nil")
	}
}

func TestLoadSkillBuffs_RequiresEndowForWeaponEndow(t *testing.T) {
	entry := skillBuffEntry{AegisName: "TK_SEVENWIND", SelfBuff: &data.SelfBuff{Name: "mild_wind", Kind: data.BuffKindWeaponEndow, Persistence: data.PersistenceTransient}}
	if err := validateSkillBuffEntry(0, entry); err == nil {
		t.Fatal("expected error for weapon_endow without endow elements, got nil")
	}
}

func TestApplySkillBuffs_ErrorsOnUnknownAegis(t *testing.T) {
	skills := []data.Skill{{ID: 411, AegisName: "TK_RUN"}}
	overlay := map[string]*data.SelfBuff{"NOPE_SKILL": {Name: "x", Kind: data.BuffKindStatus, Persistence: data.PersistencePermanent}}
	if _, err := applySkillBuffs(skills, overlay); err == nil {
		t.Fatal("expected error for aegis not in catalog, got nil")
	}
}

func TestApplySkillBuffs_MergesOntoRecord(t *testing.T) {
	skills := []data.Skill{{ID: 411, AegisName: "TK_RUN"}}
	overlay := map[string]*data.SelfBuff{"TK_RUN": {Name: "spurt", Kind: data.BuffKindStatBuff, Persistence: data.PersistenceTransient}}
	n, err := applySkillBuffs(skills, overlay)
	if err != nil || n != 1 {
		t.Fatalf("apply: n=%d err=%v", n, err)
	}
	if skills[0].SelfBuff == nil || skills[0].SelfBuff.Name != "spurt" {
		t.Fatalf("SelfBuff not merged: %+v", skills[0].SelfBuff)
	}
}

func TestLoadSkillBuffs_RejectsEndowOnNonEndow(t *testing.T) {
	entry := skillBuffEntry{AegisName: "TK_RUN", SelfBuff: &data.SelfBuff{
		Name: "spurt", Kind: data.BuffKindStatBuff, Persistence: data.PersistenceTransient,
		Endow: &data.EndowSpec{Elements: []string{"fire"}},
	}}
	if err := validateSkillBuffEntry(0, entry); err == nil {
		t.Fatal("expected error for endow set on non-weapon_endow kind, got nil")
	}
}

func TestLoadSkillBuffs_RejectsDuplicateEndowElement(t *testing.T) {
	entry := skillBuffEntry{AegisName: "TK_SEVENWIND", SelfBuff: &data.SelfBuff{
		Name: "mild_wind", Kind: data.BuffKindWeaponEndow, Persistence: data.PersistenceTransient,
		Endow: &data.EndowSpec{Elements: []string{"earth", "wind", "earth"}}, // earth repeated
	}}
	if err := validateSkillBuffEntry(0, entry); err == nil {
		t.Fatal("expected error for duplicate endow element, got nil")
	}
}

func TestLoadSkillBuffs_RejectsDuplicateBuffName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dupname.yaml")
	// Two distinct aegis_names mapping to the same self_buff.name. resolveBuffs
	// keys on the name, so a collision would silently resolve the wrong anchor.
	content := []byte("skills:\n" +
		"  - aegis_name: TK_RUN\n    self_buff:\n      name: spurt\n      kind: stat_buff\n      persistence: transient\n" +
		"  - aegis_name: TK_MISSION\n    self_buff:\n      name: spurt\n      kind: status\n      persistence: permanent\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSkillBuffs(path); err == nil {
		t.Fatal("expected error for duplicate self_buff.name, got nil")
	}
}

func TestLoadSkillBuffs_RejectsDuplicateAegis(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.yaml")
	content := []byte("skills:\n" +
		"  - aegis_name: TK_RUN\n    self_buff:\n      name: spurt\n      kind: stat_buff\n      persistence: transient\n" +
		"  - aegis_name: TK_RUN\n    self_buff:\n      name: spurt2\n      kind: stat_buff\n      persistence: transient\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSkillBuffs(path); err == nil {
		t.Fatal("expected error for duplicate aegis_name, got nil")
	}
}
