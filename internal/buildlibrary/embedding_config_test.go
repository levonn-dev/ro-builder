package buildlibrary

import (
	"context"
	"testing"
)

func TestMigration_EmbeddingConfigTableExists(t *testing.T) {
	lib := newTestLibrary(t)
	var n int
	err := lib.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name = 'embedding_config'`).Scan(&n)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("embedding_config table missing (got %d)", n)
	}
}
