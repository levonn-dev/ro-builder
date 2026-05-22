package hercules

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseFile_IntField(t *testing.T) {
	list, err := ParseFile([]byte(`x: ( { Id: 1201 } )`))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d items, want 1", len(list))
	}
	item := list[0].(map[string]any)
	if item["Id"] != 1201 {
		t.Errorf("Id: got %v (%T), want 1201 (int)", item["Id"], item["Id"])
	}
}

func TestParseFile_StringField(t *testing.T) {
	list, _ := ParseFile([]byte(`x: ( { AegisName: "Knife" } )`))
	item := list[0].(map[string]any)
	if item["AegisName"] != "Knife" {
		t.Errorf("AegisName: got %q, want \"Knife\"", item["AegisName"])
	}
}

func TestParseFile_BoolField(t *testing.T) {
	list, _ := ParseFile([]byte(`x: ( { Refine: true, Stack: false } )`))
	item := list[0].(map[string]any)
	if item["Refine"] != true {
		t.Errorf("Refine: got %v, want true", item["Refine"])
	}
	if item["Stack"] != false {
		t.Errorf("Stack: got %v, want false", item["Stack"])
	}
}

func TestParseFile_MultipleEntries(t *testing.T) {
	list, _ := ParseFile([]byte(`item_db: (
		{ Id: 501, AegisName: "Red_Potion" },
		{ Id: 502, AegisName: "Orange_Potion" }
	)`))
	if len(list) != 2 {
		t.Fatalf("got %d items, want 2", len(list))
	}
	if list[0].(map[string]any)["Id"] != 501 {
		t.Errorf("first Id mismatch")
	}
	if list[1].(map[string]any)["AegisName"] != "Orange_Potion" {
		t.Errorf("second AegisName mismatch")
	}
}

func TestParseFile_NestedObject(t *testing.T) {
	list, _ := ParseFile([]byte(`x: ( {
		Id: 1201
		Job: {
			Novice: true
			Swordsman: true
		}
	} )`))
	item := list[0].(map[string]any)
	job, ok := item["Job"].(map[string]any)
	if !ok {
		t.Fatalf("Job: got %T, want map[string]any", item["Job"])
	}
	if job["Novice"] != true || job["Swordsman"] != true {
		t.Errorf("Job: got %v", job)
	}
}

// Single-line heredoc syntax used by Hercules (`Script: <" ... ">`). Treated
// as an opaque string; we don't try to interpret the embedded language.
func TestParseFile_SingleLineHeredoc(t *testing.T) {
	list, _ := ParseFile([]byte(`x: ( {
		Id: 4043
		Script: <" bonus bBaseAtk,20; ">
	} )`))
	item := list[0].(map[string]any)
	want := " bonus bBaseAtk,20; "
	if item["Script"] != want {
		t.Errorf("Script: got %q, want %q", item["Script"], want)
	}
}

// Heredocs span newlines; multi-line scripts are common in item_combo and
// some weapon scripts. Whitespace inside is preserved verbatim.
func TestParseFile_MultiLineHeredoc(t *testing.T) {
	list, _ := ParseFile([]byte(`x: ( {
		Script: <"
			bonus bStr, 1;
			bonus bAgi, 2;
		">
	} )`))
	item := list[0].(map[string]any)
	s, ok := item["Script"].(string)
	if !ok {
		t.Fatalf("Script not a string: %T", item["Script"])
	}
	for _, want := range []string{"bonus bStr, 1;", "bonus bAgi, 2;"} {
		if !strings.Contains(s, want) {
			t.Errorf("multi-line heredoc missing %q; got %q", want, s)
		}
	}
}

func TestParseFile_LineComment(t *testing.T) {
	list, _ := ParseFile([]byte(`// top-of-file note
x: ( {
	Id: 1  // trailing comment
	AegisName: "Knife"
} )`))
	item := list[0].(map[string]any)
	if item["Id"] != 1 || item["AegisName"] != "Knife" {
		t.Errorf("comment-skip mishandled: %v", item)
	}
}

func TestParseFile_BlockComment(t *testing.T) {
	list, _ := ParseFile([]byte(`/*
header block
spanning lines
*/
x: ( { Id: 5 /* inline block */ } )`))
	item := list[0].(map[string]any)
	if item["Id"] != 5 {
		t.Errorf("block-comment skip mishandled: %v", item)
	}
}

// The full Knife pre-re entry, as it appears in Hercules. Confirms the
// parser handles a real-world record end-to-end: int / string / nested
// Job{} / multiple top-level fields with no separators between.
func TestParseFile_KnifeFullEntry(t *testing.T) {
	list, err := ParseFile([]byte(knifePreReFixture))
	if err != nil {
		t.Fatal(err)
	}
	item := list[0].(map[string]any)
	wantTopLevel := map[string]any{
		"Id":        1201,
		"AegisName": "Knife",
		"Name":      "Knife",
		"Type":      "IT_WEAPON",
		"Buy":       50,
		"Weight":    400,
		"Atk":       17,
		"Range":     1,
		"Slots":     3,
		"Loc":       "EQP_WEAPON",
		"WeaponLv":  1,
		"EquipLv":   1,
		"Subtype":   "W_DAGGER",
	}
	for k, v := range wantTopLevel {
		if !reflect.DeepEqual(item[k], v) {
			t.Errorf("Knife.%s: got %v (%T), want %v", k, item[k], item[k], v)
		}
	}
	job, _ := item["Job"].(map[string]any)
	if job == nil {
		t.Fatalf("Job missing or wrong type")
	}
	if job["Novice"] != true || job["Swordsman"] != true || job["Crusader"] != true {
		t.Errorf("Job entries: got %v", job)
	}
}

const knifePreReFixture = `item_db: (
{
	Id: 1201
	AegisName: "Knife"
	Name: "Knife"
	Type: "IT_WEAPON"
	Buy: 50
	Weight: 400
	Atk: 17
	Range: 1
	Slots: 3
	Job: {
		Novice: true
		Swordsman: true
		Magician: true
		Archer: true
		Merchant: true
		Thief: true
		Knight: true
		Wizard: true
		Blacksmith: true
		Hunter: true
		Assassin: true
		Crusader: true
		Sage: true
		Rogue: true
		Alchemist: true
		Bard: true
	}
	Loc: "EQP_WEAPON"
	WeaponLv: 1
	EquipLv: 1
	Subtype: "W_DAGGER"
}
)`
