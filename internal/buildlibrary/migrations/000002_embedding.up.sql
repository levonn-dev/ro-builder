-- Singleton config row stamping which embedding model+tier produced the
-- corpus. Model-agnostic and pgvector-free: the vector column + extension
-- are created by the app's config-driven bootstrap, not here, because
-- CREATE EXTENSION for non-trusted pgvector needs privileges the migration
-- role should not require.
CREATE TABLE embedding_config (
    id         smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    model_id   text NOT NULL,
    dimensions integer NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
