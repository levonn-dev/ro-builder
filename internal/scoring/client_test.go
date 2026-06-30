package scoring

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// --- Unit tests: HTTP shape + error categorization ---
//
// These exercise the client against an httptest stand-in so we can verify
// the JSON marshalling, error-mapping, and decode paths without booting a
// real sidecar. The end-to-end node-driven test below is what actually
// guards the contract; these are about the Go side.

func TestClient_Score_SendsExpectedJSON(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/score" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"derived":{"hit":2,"flee":2,"cri":1,"atk":{"base":1,"plus":0},"matk":{"min":1,"max":1},"def":{"hard":0,"soft":1},"mdef":{"hard":0,"soft":1},"aspd":150.3,"maxHp":40,"maxSp":11,"statPointsRemaining":48}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	_, err := c.Score(context.Background(), &ScoreRequest{
		Class: new("taekwon_kid"),
		Stats: &Stats{Str: 99, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
		Equipment: map[SlotKey]EquipSpec{
			SlotWeapon: {ID: 1201, Refine: 7, Cards: []int{4043, 4043}},
		},
		Enemy: new(1002),
	})
	if err != nil {
		t.Fatalf("score: %v", err)
	}

	// Assert the shape that crosses the wire; the sidecar reads these
	// exact JSON keys, so any rename here is a contract break.
	if got["class"] != "taekwon_kid" {
		t.Errorf("class: got %v want %q", got["class"], "taekwon_kid")
	}
	if got["enemy"] != float64(1002) {
		t.Errorf("enemy: got %v want 1002", got["enemy"])
	}
	stats := got["stats"].(map[string]any)
	if stats["str"] != float64(99) {
		t.Errorf("stats.str: got %v want 99", stats["str"])
	}
	equip := got["equipment"].(map[string]any)
	weapon := equip["weapon"].(map[string]any)
	if weapon["id"] != float64(1201) {
		t.Errorf("equipment.weapon.id: got %v want 1201", weapon["id"])
	}
	if weapon["refine"] != float64(7) {
		t.Errorf("equipment.weapon.refine: got %v want 7", weapon["refine"])
	}
}

func TestClient_Score_DecodesNullableCombatFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Sentinel response: damage min/max present, ave/secondAve null.
		// This is the JSON the sidecar emits per the shim contract when a
		// field can't be computed (the shim translates engine-specific
		// sentinel strings into JSON null).
		_, _ = w.Write([]byte(`{
			"derived": {"hit":2,"flee":2,"cri":1,"atk":{"base":1,"plus":0},"matk":{"min":1,"max":1},"def":{"hard":0,"soft":1},"mdef":{"hard":0,"soft":1},"aspd":150.3,"maxHp":40,"maxSp":11,"statPointsRemaining":48},
			"combat": {
				"damage": {"min":12, "ave":null, "max":18, "secondAve":null},
				"crit": {"damage":null, "rate":5},
				"hit": 87, "flee": 100, "dodge": null, "battleTimeSec": null,
				"incoming": {"min":50, "ave":75, "max":100, "aveWithDodge":null},
				"enemy": {"hp":1500, "race":"Demi-Human", "element":"Neutral", "size":"Medium", "type":"Normal"}
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	resp, err := c.Score(context.Background(), &ScoreRequest{})
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if resp.Combat == nil {
		t.Fatal("combat should be non-nil")
	}

	// Present fields decode to non-nil pointers with the right value.
	if resp.Combat.Damage.Min == nil || *resp.Combat.Damage.Min != 12 {
		t.Errorf("damage.min: got %v want 12", resp.Combat.Damage.Min)
	}
	// Null fields decode to nil pointers; the orchestrator branches on this
	// to detect "shim couldn't compute" rather than parsing free-form text.
	if resp.Combat.Damage.Ave != nil {
		t.Errorf("damage.ave: got %v want nil", *resp.Combat.Damage.Ave)
	}
	if resp.Combat.Crit.Damage != nil {
		t.Errorf("crit.damage: got %v want nil", *resp.Combat.Crit.Damage)
	}
	if resp.Combat.Dodge != nil {
		t.Errorf("dodge: got %v want nil", *resp.Combat.Dodge)
	}
	if resp.Combat.Enemy.HP == nil || *resp.Combat.Enemy.HP != 1500 {
		t.Errorf("enemy.hp: got %v want 1500", resp.Combat.Enemy.HP)
	}
}

func TestClient_Score_ClassifiesHTTPStatus(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantMsg   string
		wantClass bool // expect IsClient() == true
	}{
		{"400 with error body", 400, `{"error":"unmapped iRO id 99999"}`, "unmapped iRO id 99999", true},
		{"500 with error body", 500, `{"error":"shim panicked"}`, "shim panicked", false},
		{"400 with garbage body", 400, `not json`, "not json", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := NewClient(srv.URL, nil)
			_, err := c.Score(context.Background(), &ScoreRequest{})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var sErr *Error
			if !errors.As(err, &sErr) {
				t.Fatalf("expected *Error, got %T: %v", err, err)
			}
			if sErr.Status != tc.status {
				t.Errorf("status: got %d want %d", sErr.Status, tc.status)
			}
			if !strings.Contains(sErr.Message, tc.wantMsg) {
				t.Errorf("message: got %q want substring %q", sErr.Message, tc.wantMsg)
			}
			if sErr.IsClient() != tc.wantClass {
				t.Errorf("IsClient: got %v want %v", sErr.IsClient(), tc.wantClass)
			}
			if IsClientError(err) != tc.wantClass {
				t.Errorf("IsClientError: got %v want %v", IsClientError(err), tc.wantClass)
			}
		})
	}
}

// --- End-to-end: boot real sidecar, hit it, verify ---
//
// The integration test boots `node server.ts` with PORT=0 (OS-assigned port)
// and reads the actual port off the banner the sidecar logs after binding.
// This catches contract drift between the Go types here and types.ts in the
// sidecar; JSON renames on either side fail the round-trip.
//
// Skipped when `node` isn't on PATH so contributors without a Node toolchain
// aren't blocked from running the rest of the suite.

const sidecarRelativePath = "../../calc-sidecar"

// startSidecar boots the calc-sidecar in a child process and returns its
// base URL plus a teardown func. The first call typically takes a few
// seconds because the shim's backend data needs to load and initialize.
func startSidecar(t *testing.T) (baseURL string, stop func()) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("node not on PATH: %v", err)
	}
	sidecarDir, err := filepath.Abs(sidecarRelativePath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "node", "server.ts")
	cmd.Dir = sidecarDir
	cmd.Env = append(cmd.Environ(), "PORT=0")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start node sidecar: %v", err)
	}

	// Drain stderr so a stalled stderr buffer can't block the process.
	go func() { _, _ = io.Copy(io.Discard, stderr) }()

	// The sidecar prints `calc-sidecar listening on :PORT` once bound. With
	// PORT=0 the printed port is the OS-assigned one, not zero; that's the
	// reason for the server.address() lookup in server.ts.
	portRe := regexp.MustCompile(`listening on :(\d+)`)
	scanner := bufio.NewScanner(stdout)
	portCh := make(chan string, 1)
	go func() {
		defer close(portCh)
		seen := false
		for scanner.Scan() {
			line := scanner.Text()
			if !seen {
				if m := portRe.FindStringSubmatch(line); m != nil {
					portCh <- m[1]
					seen = true
					// Don't return; keep draining stdout so the sidecar's
					// stdout pipe doesn't fill and block its writer. The
					// loop exits naturally when cmd.Wait() closes the pipe.
				}
			}
			// After the banner: line is read and discarded. Effective
			// io.Discard via the Scanner reading + not buffering.
		}
	}()

	var port string
	select {
	case p, ok := <-portCh:
		if !ok {
			cancel()
			_ = cmd.Wait()
			t.Fatal("sidecar exited before printing port banner")
		}
		port = p
	case <-time.After(30 * time.Second):
		cancel()
		_ = cmd.Wait()
		t.Fatal("timeout waiting for sidecar port banner")
	}

	stop = func() {
		cancel()
		_ = cmd.Wait()
	}
	return "http://127.0.0.1:" + port, stop
}

func TestIntegration_DefaultNoviceMatchesShimBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in -short mode")
	}
	baseURL, stop := startSidecar(t)
	defer stop()

	c := NewClient(baseURL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := c.Score(ctx, &ScoreRequest{})
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	// Same baseline shim.test.ts asserts at default Novice / lvl 1 / stats 1.
	// If the shim's backend data shifts, both this and the TS contract
	// test need updating in lock-step; that lock-step is the point.
	want := DerivedStats{
		Hit:                 2,
		Flee:                2,
		Cri:                 1,
		Atk:                 BasePlus{Base: 1, Plus: 0},
		Matk:                MinMax{Min: 1, Max: 1},
		Def:                 HardSoft{Hard: 0, Soft: 1},
		Mdef:                HardSoft{Hard: 0, Soft: 1},
		Aspd:                150.3,
		MaxHP:               40,
		MaxSP:               11,
		StatPointsRemaining: 48,
		// Default Novice with all stats at 1; gear + cards = none, so
		// TotalStats matches the raw stat allocation exactly.
		TotalStats: Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
	}
	if resp.Derived != want {
		t.Errorf("derived: got %+v want %+v", resp.Derived, want)
	}
	if resp.Combat != nil {
		t.Errorf("combat: should be nil (no enemy in request), got %+v", resp.Combat)
	}
}

func TestIntegration_BadIRoIDReturns4xx(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in -short mode")
	}
	baseURL, stop := startSidecar(t)
	defer stop()

	c := NewClient(baseURL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 999999999 is an unmapped iRO id; sidecar must reject with 400.
	_, err := c.Score(ctx, &ScoreRequest{
		Equipment: map[SlotKey]EquipSpec{
			SlotWeapon: {ID: 999999999},
		},
	})
	if err == nil {
		t.Fatal("expected error for unmapped iRO id, got nil")
	}
	if !IsClientError(err) {
		t.Errorf("expected 4xx client error, got: %v", err)
	}
}
