// Command embedding-recall reports mean recall@k of the HNSW index vs exact
// KNN over a sample of stored vectors. 0.98 => leave the index defaults;
// 0.85 => raise EMBEDDING_HNSW_M / EF_CONSTRUCTION (then re-backfill) or
// EMBEDDING_HNSW_EF_SEARCH (--ef-search; no rebuild). Run after a backfill.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/embedding"
)

func main() {
	k := flag.Int("k", 10, "neighbors per query")
	sample := flag.Int("sample", 50, "number of probe vectors")
	efSearch := flag.Int("ef-search", 0, "hnsw.ef_search override (0 = server default)")
	flag.Parse()
	if err := run(*k, *sample, *efSearch); err != nil {
		fmt.Fprintln(os.Stderr, "embedding-recall:", err)
		os.Exit(1)
	}
}

func run(k, sample, efSearch int) error {
	dsn := os.Getenv("DATABASE_URL")
	cfg := embedding.LoadConfigFromEnv()
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL must be set")
	}
	if !cfg.Enabled() {
		return fmt.Errorf("embedding not configured: EMBEDDING_* env vars must be set")
	}
	ctx := context.Background()
	lib, err := buildlibrary.Open(ctx, dsn, buildlibrary.WithEmbedding(cfg.Dim, cfg.Model+"@"+strconv.Itoa(cfg.Dim)))
	if err != nil {
		return err
	}
	defer func() { _ = lib.Close() }()
	if !lib.SemanticEnabled() {
		return fmt.Errorf("library is recency-only; nothing to measure")
	}
	probes, err := lib.SampleVectors(ctx, sample)
	if err != nil {
		return err
	}
	r, err := lib.RecallAtK(ctx, probes, k, efSearch)
	if err != nil {
		return err
	}
	fmt.Printf("recall@%d over %d probes (ef_search=%d): %.4f\n", k, len(probes), efSearch, r)
	return nil
}
