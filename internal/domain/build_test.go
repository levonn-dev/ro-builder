package domain

import (
	"strings"
	"testing"
)

func TestBuild_Validate(t *testing.T) {
	cases := []struct {
		name    string
		build   Build
		wantErr string
	}{
		{name: "minimal valid", build: Build{Mode: PreRenewal}},
		{name: "empty mode treated as pre-renewal", build: Build{Class: "novice"}},
		{name: "named class accepted without lookup", build: Build{Class: "taekwon_kid"}},
		{name: "renewal rejected", build: Build{Mode: Renewal}, wantErr: "not supported"},
		{name: "unknown mode rejected", build: Build{Mode: "kRO"}, wantErr: "unknown mode"},
		{name: "refine over 10 rejected", build: Build{
			Class:     "knight",
			Equipment: map[SlotKey]EquipSpec{"weapon": {ID: 1201, Refine: 11}},
		}, wantErr: "refine 11"},
		{name: "negative level rejected", build: Build{
			Class: "knight", Level: Level{Base: -1},
		}, wantErr: "must be >= 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("Validate: got %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate: got nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate: got %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestBuild_ToScoreRequest(t *testing.T) {
	t.Run("empty class produces nil Class pointer", func(t *testing.T) {
		// Empty Class lets the shim's reset baseline apply (Novice).
		b := Build{Mode: PreRenewal}
		req := b.ToScoreRequest(nil, nil)
		if req.Class != nil {
			t.Errorf("class: got %q, want nil (empty Class should be omitted)", *req.Class)
		}
	})

	t.Run("named class round-trips as string pointer", func(t *testing.T) {
		b := Build{Mode: PreRenewal, Class: "taekwon_kid"}
		req := b.ToScoreRequest(nil, nil)
		if req.Class == nil || *req.Class != "taekwon_kid" {
			t.Errorf("class: got %v, want %q", req.Class, "taekwon_kid")
		}
	})

	t.Run("zero level/stats produce nil pointers", func(t *testing.T) {
		b := Build{Mode: PreRenewal, Class: "taekwon_kid"}
		req := b.ToScoreRequest(nil, nil)
		if req.Level != nil {
			t.Errorf("level: got %+v, want nil (zero-value Level should be skipped)", *req.Level)
		}
		if req.Stats != nil {
			t.Errorf("stats: got %+v, want nil (zero-value Stats should be skipped)", *req.Stats)
		}
		if req.Equipment != nil {
			t.Errorf("equipment: got %+v, want nil", req.Equipment)
		}
		if req.Enemy != nil {
			t.Errorf("enemy: got %d, want nil (no scenario)", *req.Enemy)
		}
	})

	t.Run("populated build round-trips through pointers", func(t *testing.T) {
		b := Build{
			Mode:   PreRenewal,
			Server: "uaro",
			Tier:   TierBudget,
			Class:  "taekwon_kid",
			Level:  Level{Base: 70, Job: 50},
			Stats:  Stats{Str: 1, Agi: 99, Vit: 1, Int: 1, Dex: 50, Luk: 1},
			Equipment: map[SlotKey]EquipSpec{
				"weapon": {ID: 1201, Refine: 7},
			},
		}
		s := &Scenario{Target: 1002}
		req := b.ToScoreRequest(s, nil)

		if *req.Class != "taekwon_kid" || req.Level.Base != 70 || req.Stats.Agi != 99 {
			t.Errorf("conversion lost data: %+v", req)
		}
		if len(req.Equipment) != 1 || req.Equipment["weapon"].Refine != 7 {
			t.Errorf("equipment lost: %+v", req.Equipment)
		}
		if req.Enemy == nil || *req.Enemy != 1002 {
			t.Errorf("enemy: got %v, want 1002", req.Enemy)
		}
	})

	t.Run("scenario target=0 is omitted", func(t *testing.T) {
		b := Build{Class: "novice"}
		req := b.ToScoreRequest(&Scenario{Target: 0}, nil)
		if req.Enemy != nil {
			t.Errorf("enemy: got %d, want nil (target=0 should mean no combat sim)", *req.Enemy)
		}
	})
}
