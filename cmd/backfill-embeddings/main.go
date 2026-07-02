// Command backfill-embeddings (re-)embeds every saved trajectory under the
// currently configured EMBEDDING_* model and re-stamps embedding_config.
// Run it after a model/tier change, or to populate rows saved while the
// embedder was unavailable. It opens the library with WithEmbedding, which
// runs the bootstrap (recreating the column at the new dimension when the
// corpus is empty); for a model change against a non-empty corpus, TRUNCATE
// or accept that mixed vectors are overwritten in place here.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/embedding"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "backfill-embeddings:", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	cfg := embedding.LoadConfigFromEnv()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !cfg.Enabled() {
		return fmt.Errorf("EMBEDDING_BASE_URL must be set to backfill")
	}
	modelID := cfg.Model + "@" + strconv.Itoa(cfg.Dim)
	ctx := context.Background()
	lib, err := buildlibrary.Open(ctx, dsn, buildlibrary.WithEmbedding(cfg.Dim, modelID))
	if err != nil {
		return err
	}
	defer func() { _ = lib.Close() }()
	if !lib.SemanticEnabled() {
		return fmt.Errorf("library is recency-only after open (mismatch vs existing corpus). " +
			"To change models, TRUNCATE saved_trajectories.embedding first or run against an empty corpus")
	}
	emb := embedding.New(cfg)
	ids, err := lib.AllIDs(ctx)
	if err != nil {
		return err
	}
	for i, id := range ids {
		doc, err := lib.DocumentText(ctx, id)
		if err != nil {
			return fmt.Errorf("document %s: %w", id, err)
		}
		vecs, err := emb.Embed(ctx, []string{doc})
		if err != nil {
			return fmt.Errorf("embed %s: %w", id, err)
		}
		if err := lib.SetEmbedding(ctx, id, vecs[0]); err != nil {
			return fmt.Errorf("store %s: %w", id, err)
		}
		fmt.Printf("[%d/%d] %s\n", i+1, len(ids), id)
	}
	fmt.Printf("backfilled %d rows for %s\n", len(ids), modelID)
	return nil
}
