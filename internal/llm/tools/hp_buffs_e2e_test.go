package tools

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

const sidecarRelativePath = "../../../calc-sidecar"

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

	// Wait for the sidecar to be ready to accept requests.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:" + port + "/score")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	stop = func() {
		cancel()
		_ = cmd.Wait()
	}
	return "http://127.0.0.1:" + port, stop
}

// TestIntegration_HPBuffs_RaiseScoredOffenseVsUndead proves the High Priest
// buff chain (blessing + impositio_manus + lex_aeterna) flows through the
// full path -- overlay, resolver, contract, sidecar drivers, calc -- and
// raises scored damage against an undead target. Boots its own sidecar.
// Skipped under -short.
func TestIntegration_HPBuffs_RaiseScoredOffenseVsUndead(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: needs running sidecar")
	}
	baseURL, stop := startSidecar(t)
	defer stop()

	client := scoring.NewClient(baseURL, nil)
	cat := loadCat(t)

	// Undead leveling target so Aspersio (holy) + Signum + Lex all bite.
	enemy := &scoring.EnemyStats{
		Hp: 200000, AtkMin: 100, AtkMax: 200, Def: 30, MDef: 10,
		Race: "RC_Undead", Element: "Ele_Undead", ElementLv: 1, Size: "Size_Medium", Level: 95,
	}
	base := &domain.Snapshot{
		Class: "high_priest",
		Level: domain.Level{Base: 99, Job: 70},
		Stats: domain.Stats{Str: 90, Agi: 40, Vit: 60, Int: 40, Dex: 99, Luk: 20},
		Skills: []domain.SkillAlloc{
			{ID: 34, Level: 10}, // AL_BLESSING
			{ID: 66, Level: 5},  // PR_IMPOSITIO
		},
	}
	reqBase := base.ToScoreRequest(nil)
	reqBase.EnemyInline = enemy
	respBase, err := client.Score(context.Background(), reqBase)
	if err != nil {
		t.Fatal(err)
	}

	buffed := *base
	lexID := buffAnchorID(t, cat, "high_priest", "lex_aeterna")
	buffed.Skills = append(append([]domain.SkillAlloc{}, base.Skills...),
		domain.SkillAlloc{ID: lexID, Level: 1})
	buffed.ActiveBuffs = []domain.ActiveBuff{
		{Name: "blessing"},
		{Name: "impositio_manus"},
		{Name: "lex_aeterna"},
	}
	reqBuff := buffed.ToScoreRequest(nil)
	reqBuff.EnemyInline = enemy
	resolved, err := resolveBuffs("high_priest", buffed.Skills, buffed.ActiveBuffs, cat)
	if err != nil {
		t.Fatal(err)
	}
	reqBuff.Buffs = resolved
	respBuff, err := client.Score(context.Background(), reqBuff)
	if err != nil {
		t.Fatal(err)
	}

	if respBase.Combat == nil || respBuff.Combat == nil {
		t.Fatal("combat results missing (enemy not applied?)")
	}
	// CombatDamage.Ave is *float64 (nil when the hit rate is unsolvable).
	baseAve, buffAve := respBase.Combat.Damage.Ave, respBuff.Combat.Damage.Ave
	if baseAve == nil || buffAve == nil {
		t.Fatalf("combat damage.ave is nil (unsolvable hit rate); raise dex/lower target def. base=%v buffed=%v", baseAve, buffAve)
	}
	t.Logf("damage.ave: base=%.1f buffed=%.1f", *baseAve, *buffAve)
	if !(*buffAve > *baseAve) {
		t.Fatalf("buffs did not raise scored damage: base=%v buffed=%v", *baseAve, *buffAve)
	}
}
