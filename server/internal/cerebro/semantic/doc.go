// CEREBRO-NEW-FILE(search-semantic-fir-2604): semantic search support for
// the FIR-2604 hybrid (FTS + vector) pipeline.
//
// Package semantic owns three things:
//
//  1. The Provider interface that returns an embedding for a piece of text.
//     Two concrete providers ship in-tree: an OpenAI-compatible HTTP client
//     for production and a deterministic in-memory provider for tests.
//
//  2. The background worker that drains cerebro_search_embedding_queue,
//     embeds the freshest version of each target, and upserts the row into
//     cerebro_search_embedding. Triggers in migration 9056 keep the queue
//     populated on every issue/comment write — the worker is the only thing
//     that needs to know how to talk to a model.
//
//  3. The query-side helper that returns the top-N target ids closest to a
//     query embedding under cosine distance, scoped to a workspace. The
//     handler combines this list with the FTS rank list via Reciprocal Rank
//     Fusion in package search (cerebrosearch.BuildHybrid).
//
// pgvector is optional. The migration skips the embedding store when the
// extension is missing; this package treats a missing store the same as a
// disabled provider — the search code falls back to keyword-only.
package semantic
