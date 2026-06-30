package buildlibrary

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testDSN points at the shared package-level Postgres container. Empty
// under -short (no container started).
var testDSN string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "pgvector/pgvector:pg17",
		postgres.WithDatabase("robuilder_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testcontainers postgres start:", err)
		os.Exit(1)
	}
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "connection string:", err)
		os.Exit(1)
	}
	testDSN = dsn
	code := m.Run()
	_ = ctr.Terminate(ctx)
	os.Exit(code)
}

// newTestLibrary opens a Library against the shared container and
// truncates both tables so each test starts clean. Skips under -short.
func newTestLibrary(t *testing.T) *Library {
	t.Helper()
	if testing.Short() {
		t.Skip("requires Postgres (testcontainers); skipped under -short")
	}
	lib, err := Open(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	if err := lib.Truncate(context.Background()); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return lib
}

func TestOpen_AppliesMigrationsAndCountsEmpty(t *testing.T) {
	lib := newTestLibrary(t)
	if err := lib.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	n, err := lib.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Fatalf("Count = %d, want 0", n)
	}
}

func TestOpen_RejectsEmptyDSN(t *testing.T) {
	if _, err := Open(context.Background(), ""); err == nil {
		t.Error("expected error from Open with empty dsn")
	}
}
