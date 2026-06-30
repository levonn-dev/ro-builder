package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

func openTempLibrary(t *testing.T) *buildlibrary.Library {
	t.Helper()
	if testing.Short() {
		t.Skip("requires Postgres (testcontainers); skipped under -short")
	}
	lib, err := buildlibrary.Open(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("buildlibrary.Open: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	if err := lib.Truncate(context.Background()); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return lib
}

func loadCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	return cat
}

// buildsServer mounts a Server with the given library + catalog onto a
// fresh httptest.Server. Returns the server URL so tests can issue real
// HTTP GETs (useful because the path /builds/{id} relies on net/http's
// pattern matching).
func buildsServer(t *testing.T, lib *buildlibrary.Library, cat *catalog.Catalog) *httptest.Server {
	t.Helper()
	api := NewServer(scoring.NewClient("http://localhost:0", nil), cat).WithLibrary(lib)
	mux := http.NewServeMux()
	api.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// seedSavedBuild inserts a small TK trajectory referencing real catalog
// item / card / skill ids so the enrichment has something to resolve.
// Returns the saved id. Snapshot carries a passing gate so the library's
// save guard accepts it. Uses Enqueue + ClaimNext + SaveAndComplete so
// the generation row ends up in status=completed, matching what a real
// worker produces.
func seedSavedBuild(t *testing.T, lib *buildlibrary.Library) string {
	t.Helper()
	ctx := context.Background()
	id, err := lib.Enqueue(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Claim the job so SaveAndComplete can mark it completed.
	// SaveAndComplete requires status='running' and a matching lease_owner.
	const testOwner = "test-worker"
	gen, err := lib.ClaimNext(ctx, testOwner, 30*time.Second)
	if err != nil || gen == nil {
		t.Fatalf("ClaimNext: gen=%v err=%v", gen, err)
	}
	in := buildlibrary.SaveInput{
		ID:        id,
		Owner:     testOwner,
		Class:     "taekwon_kid",
		Server:    "uaro",
		Playstyle: "pvm",
		Mode:      "pre-renewal",
		Primary: domain.Trajectory{
			Class: "taekwon_kid",
			Snapshots: []domain.Snapshot{
				{
					Class: "taekwon_kid",
					Level: domain.Level{Base: 99, Job: 50},
					Stats: domain.Stats{Str: 95, Agi: 80, Vit: 20, Int: 1, Dex: 50, Luk: 4},
					Equipment: map[domain.SlotKey]domain.EquipSpec{
						// Item 5100 = Sales Banner, card 4042 = Steel
						// Chonchon Card; using the same ids the user's
						// run exposed so the test surfaces those same
						// catalog names in the enrichment.
						"headTop": {ID: 5100, Cards: []int{4042}},
						"armor":   {ID: 2357, Refine: 7, Cards: []int{4147}},
					},
					Skills: []domain.SkillAlloc{
						{ID: 411, Level: 10}, // TK_RUN / Running
						{ID: 413, Level: 7},  // TK_STORMKICK / Tornado Kick
					},
					Score: &scoring.ScoreResponse{
						Derived: scoring.DerivedStats{MaxHP: 3811},
					},
					Gates: []domain.GateResult{
						{Name: "requirements_met", Severity: domain.GateSeverityPass},
					},
				},
			},
		},
		FinalText: "test build",
	}
	if _, err := lib.SaveAndComplete(ctx, in); err != nil {
		t.Fatalf("SaveAndComplete: %v", err)
	}
	return id
}

func TestGetBuild_ReturnsEnrichedTrajectory(t *testing.T) {
	lib := openTempLibrary(t)
	cat := loadCatalog(t)
	id := seedSavedBuild(t, lib)
	srv := buildsServer(t, lib, cat)

	resp, err := http.Get(srv.URL + "/builds/" + id)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d want 200, body=%s", resp.StatusCode, raw)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body["id"] != id {
		t.Errorf("id mismatch: got %v want %s", body["id"], id)
	}
	if body["class"] != "taekwon_kid" {
		t.Errorf("class: got %v want taekwon_kid", body["class"])
	}
	if _, ok := body["catalog_names"]; ok {
		t.Errorf("catalog_names should be gone from the wire shape; got %+v", body["catalog_names"])
	}

	primary, _ := body["primary"].(map[string]any)
	snapshots, _ := primary["snapshots"].([]any)
	if len(snapshots) == 0 {
		t.Fatalf("expected at least one snapshot, got %+v", primary)
	}
	snap, _ := snapshots[0].(map[string]any)

	// Equipment.headTop.item.id == 5100, name contains "sales banner".
	equipment, _ := snap["equipment"].(map[string]any)
	headTop, _ := equipment["headTop"].(map[string]any)
	item, _ := headTop["item"].(map[string]any)
	if got, _ := item["id"].(float64); int(got) != 5100 {
		t.Errorf("equipment.headTop.item.id: got %v want 5100", item["id"])
	}
	if name, _ := item["name"].(string); !strings.Contains(strings.ToLower(name), "sales banner") {
		t.Errorf("equipment.headTop.item.name: got %q, want substring \"sales banner\"", name)
	}

	// Cards under headTop: id 4042 with non-empty name.
	cards, _ := headTop["cards"].([]any)
	if len(cards) == 0 {
		t.Fatalf("expected at least one card under headTop, got %+v", headTop)
	}
	card0, _ := cards[0].(map[string]any)
	if got, _ := card0["id"].(float64); int(got) != 4042 {
		t.Errorf("headTop.cards[0].id: got %v want 4042", card0["id"])
	}
	if name, _ := card0["name"].(string); name == "" {
		t.Errorf("headTop.cards[0].name should be resolved; got empty")
	}

	// Skills: flat shape (id, name, level). At least one entry with a
	// non-empty name.
	skills, _ := snap["skills"].([]any)
	if len(skills) == 0 {
		t.Fatalf("expected at least one skill, got %+v", snap)
	}
	skill0, _ := skills[0].(map[string]any)
	if _, ok := skill0["id"]; !ok {
		t.Errorf("skill entry missing id: %+v", skill0)
	}
	if _, ok := skill0["level"]; !ok {
		t.Errorf("skill entry missing level: %+v", skill0)
	}
	if name, _ := skill0["name"].(string); name == "" {
		t.Errorf("skill entry should carry resolved name; got empty in %+v", skill0)
	}
}

func TestGetBuild_NotFound(t *testing.T) {
	lib := openTempLibrary(t)
	cat := loadCatalog(t)
	srv := buildsServer(t, lib, cat)

	resp, err := http.Get(srv.URL + "/builds/nonexistent")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
}

func TestEnrichSavedTrajectory_NilCatalogSkipsEnrichment(t *testing.T) {
	st := &buildlibrary.SavedTrajectory{
		ID:    "abc",
		Class: "novice",
		Primary: domain.Trajectory{
			Class: "novice",
			Snapshots: []domain.Snapshot{{
				Class: "novice",
				Level: domain.Level{Base: 1, Job: 1},
				Equipment: map[domain.SlotKey]domain.EquipSpec{
					"weapon": {ID: 1201},
				},
				Skills:         []domain.SkillAlloc{{ID: 1, Level: 1}},
				LevelingTarget: &domain.Scenario{Target: 1002},
			}},
		},
	}
	out := enrichSavedTrajectory(st, nil)

	if out.ID != "abc" {
		t.Errorf("id passthrough: got %q want %q", out.ID, "abc")
	}
	if len(out.Primary.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(out.Primary.Snapshots))
	}
	snap := out.Primary.Snapshots[0]

	weapon := snap.Equipment["weapon"]
	if weapon.Item.ID != 1201 {
		t.Errorf("weapon.item.id: got %d want 1201", weapon.Item.ID)
	}
	if weapon.Item.Name != "" {
		t.Errorf("weapon.item.name with nil catalog: got %q want empty", weapon.Item.Name)
	}
	if len(snap.Skills) != 1 || snap.Skills[0].ID != 1 || snap.Skills[0].Name != "" {
		t.Errorf("skill should keep id, have empty name with nil catalog; got %+v", snap.Skills)
	}
	if snap.LevelingTarget == nil || snap.LevelingTarget.Target == nil {
		t.Fatalf("leveling_target.target should be non-nil; got %+v", snap.LevelingTarget)
	}
	if snap.LevelingTarget.Target.ID != 1002 || snap.LevelingTarget.Target.Name != "" {
		t.Errorf("leveling_target.target: got %+v want {1002, \"\"}", *snap.LevelingTarget.Target)
	}
}

func TestEnrichSavedTrajectory_HandlesUnknownIds(t *testing.T) {
	cat := loadCatalog(t)
	st := &buildlibrary.SavedTrajectory{
		ID:    "abc",
		Class: "novice",
		Primary: domain.Trajectory{
			Class: "novice",
			Snapshots: []domain.Snapshot{{
				Class: "novice",
				Level: domain.Level{Base: 1, Job: 1},
				Equipment: map[domain.SlotKey]domain.EquipSpec{
					"weapon": {ID: 99999999}, // Not in catalog.
				},
				Skills: []domain.SkillAlloc{{ID: 88888888, Level: 1}},
			}},
		},
	}
	out := enrichSavedTrajectory(st, cat)

	snap := out.Primary.Snapshots[0]
	weapon := snap.Equipment["weapon"]
	if weapon.Item.ID != 99999999 {
		t.Errorf("unknown item id should pass through; got %d", weapon.Item.ID)
	}
	if weapon.Item.Name != "" {
		t.Errorf("unknown item name should be empty; got %q", weapon.Item.Name)
	}
	if len(snap.Skills) != 1 || snap.Skills[0].ID != 88888888 || snap.Skills[0].Name != "" {
		t.Errorf("unknown skill: got %+v want {id:88888888, name:\"\", level:1}", snap.Skills)
	}
}

func TestListBuilds_ReturnsSavedTrajectories(t *testing.T) {
	lib := openTempLibrary(t)
	cat := loadCatalog(t)
	seedSavedBuild(t, lib)
	seedSavedBuild(t, lib) // two records; confirm the list is plural
	srv := buildsServer(t, lib, cat)

	resp, err := http.Get(srv.URL + "/builds")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	var body struct {
		Builds []map[string]any `json:"builds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Builds) != 2 {
		t.Errorf("expected 2 builds, got %d: %+v", len(body.Builds), body.Builds)
	}
	for _, b := range body.Builds {
		if b["id"] == "" {
			t.Errorf("build summary missing id: %+v", b)
		}
		if b["class"] != "taekwon_kid" {
			t.Errorf("class: got %v want taekwon_kid", b["class"])
		}
	}
}

func TestListBuilds_FiltersByClass(t *testing.T) {
	lib := openTempLibrary(t)
	cat := loadCatalog(t)
	seedSavedBuild(t, lib) // class taekwon_kid
	noviceID, err := lib.Enqueue(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Enqueue novice: %v", err)
	}
	other := buildlibrary.SaveInput{
		ID:        noviceID,
		Class:     "novice",
		Server:    "uaro",
		Playstyle: "pvm",
		Mode:      "pre-renewal",
		Primary: domain.Trajectory{
			Class: "novice",
			Snapshots: []domain.Snapshot{{
				Class: "novice",
				Level: domain.Level{Base: 1, Job: 1},
				Stats: domain.Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
				Score: &scoring.ScoreResponse{Derived: scoring.DerivedStats{MaxHP: 40}},
				Gates: []domain.GateResult{{Name: "requirements_met", Severity: domain.GateSeverityPass}},
			}},
		},
	}
	if _, err := lib.Save(context.Background(), other); err != nil {
		t.Fatalf("seed novice: %v", err)
	}
	srv := buildsServer(t, lib, cat)

	resp, err := http.Get(srv.URL + "/builds?class=taekwon_kid")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Builds []map[string]any `json:"builds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Builds) != 1 {
		t.Errorf("expected 1 taekwon_kid build, got %d (filter didn't apply): %+v", len(body.Builds), body.Builds)
	}
	if len(body.Builds) > 0 && body.Builds[0]["class"] != "taekwon_kid" {
		t.Errorf("filtered class: got %v want taekwon_kid", body.Builds[0]["class"])
	}
}

func TestListBuilds_EmptyReturnsEmptyArray(t *testing.T) {
	lib := openTempLibrary(t)
	cat := loadCatalog(t)
	srv := buildsServer(t, lib, cat)

	resp, err := http.Get(srv.URL + "/builds")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	// Must be an empty array, not null; clients iterate without
	// null-checks. The explicit JSON contains is the safest assertion;
	// decoding to a struct would mask `null` vs `[]`.
	if !strings.Contains(string(raw), `"builds":[]`) {
		t.Errorf("empty list should emit \"builds\":[], got %s", raw)
	}
}

func TestListBuilds_InvalidLimitReturns400(t *testing.T) {
	lib := openTempLibrary(t)
	cat := loadCatalog(t)
	srv := buildsServer(t, lib, cat)

	resp, err := http.Get(srv.URL + "/builds?limit=not-a-number")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
}

// Confirm the library's NotFound sentinel is the one we expect for the
// 404 path; guards against the sentinel being renamed without the
// handler being updated in lockstep.
func TestBuildLibrary_NotFoundSentinelMatches(t *testing.T) {
	lib := openTempLibrary(t)
	_, err := lib.Get(context.Background(), "nonexistent")
	if !errors.Is(err, buildlibrary.ErrNotFound) {
		t.Errorf("expected ErrNotFound from missing Get; got %v", err)
	}
}
