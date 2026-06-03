package domain

import "testing"

func TestSnapshotValidate_ActiveBuffs(t *testing.T) {
	good := Snapshot{
		Class: "taekwon_kid", Level: Level{Base: 99, Job: 50},
		ActiveBuffs: []ActiveBuff{{Name: "taekwon_ranker"}, {Name: "mild_wind", Element: "holy"}},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	bad := Snapshot{
		Class: "taekwon_kid", Level: Level{Base: 99, Job: 50},
		ActiveBuffs: []ActiveBuff{{Name: ""}},
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for empty buff name, got nil")
	}
}

func TestSnapshotValidate_RejectsDuplicateActiveBuff(t *testing.T) {
	s := Snapshot{
		Class: "taekwon_kid", Level: Level{Base: 99, Job: 50},
		ActiveBuffs: []ActiveBuff{{Name: "taekwon_ranker"}, {Name: "taekwon_ranker"}},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for duplicate active_buff name, got nil")
	}
}

func TestSnapshotValidate_RejectsDuplicateSkillID(t *testing.T) {
	s := Snapshot{
		Class: "taekwon_kid", Level: Level{Base: 99, Job: 50},
		Skills: []SkillAlloc{{ID: 425, Level: 7}, {ID: 425, Level: 1}},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for duplicate skill id, got nil")
	}
}
