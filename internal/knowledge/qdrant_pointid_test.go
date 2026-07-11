package knowledge

import (
	"testing"

	"github.com/google/uuid"
)

// TestQdrantPointID_AlreadyValidIDs_PassThroughUnchanged proves a caller
// that already mints Qdrant-valid point IDs (an unsigned integer, or a real
// UUID) sees no behaviour change from this fix.
func TestQdrantPointID_AlreadyValidIDs_PassThroughUnchanged(t *testing.T) {
	cases := []string{
		"0",
		"1",
		"12345",
		"18446744073709551615", // max uint64
		uuid.NewString(),
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8", // well-known RFC 4122 UUID
	}
	for _, id := range cases {
		if got := qdrantPointID(id); got != id {
			t.Errorf("qdrantPointID(%q) = %q, want unchanged (already-valid ID)", id, got)
		}
		if !isValidQdrantPointID(id) {
			t.Errorf("isValidQdrantPointID(%q) = false, want true", id)
		}
	}
}

// TestQdrantPointID_ChunkerShapedIDs_MappedToValidUUID proves the exact
// defect class this fix closes: FixedSizeChunker's "<uuid>-<position>"
// shape (see chunker.go Chunk.ID) is mapped to a Qdrant-valid UUID, is
// deterministic (idempotent re-ingestion), and never collides between two
// different semantic IDs in this test's sample.
func TestQdrantPointID_ChunkerShapedIDs_MappedToValidUUID(t *testing.T) {
	docID := uuid.NewString()
	cases := []string{
		docID + "-0",
		docID + "-1",
		docID + "-42",
		"not-a-uuid-at-all-0",
		"",
	}

	seen := make(map[string]string, len(cases))
	for _, id := range cases {
		mapped := qdrantPointID(id)
		if _, err := uuid.Parse(mapped); err != nil {
			t.Errorf("qdrantPointID(%q) = %q, not a valid UUID: %v", id, mapped, err)
		}
		// Determinism: calling again with the SAME input yields the SAME
		// output (idempotent re-ingestion — re-upserting an edited
		// document overwrites the same Qdrant points, no duplicate leak).
		if again := qdrantPointID(id); again != mapped {
			t.Errorf("qdrantPointID(%q) is non-deterministic: %q then %q", id, mapped, again)
		}
		if prior, ok := seen[mapped]; ok && prior != id {
			t.Errorf("qdrantPointID collision: %q and %q both mapped to %q", prior, id, mapped)
		}
		seen[mapped] = id
	}
}

// TestQdrantPointID_EmptyID_TreatedAsInvalid proves an empty chunk ID
// (which is never Qdrant-valid) is still mapped to SOME valid UUID rather
// than being sent to Qdrant as an empty string.
func TestQdrantPointID_EmptyID_TreatedAsInvalid(t *testing.T) {
	if isValidQdrantPointID("") {
		t.Fatal("isValidQdrantPointID(\"\") = true, want false")
	}
	mapped := qdrantPointID("")
	if _, err := uuid.Parse(mapped); err != nil {
		t.Fatalf("qdrantPointID(\"\") = %q, not a valid UUID: %v", mapped, err)
	}
}

// TestChunkFromMetadata_RoundTripsSemanticChunkID proves Search's
// reconstruction path recovers the ORIGINAL semantic chunk ID from the
// payload, not Qdrant's internal (possibly UUIDv5-derived) point ID.
func TestChunkFromMetadata_RoundTripsSemanticChunkID(t *testing.T) {
	semanticID := uuid.NewString() + "-0"
	pointID := qdrantPointID(semanticID)

	meta := map[string]any{
		"content":               "hello world",
		"document_id":           "doc-1",
		"position":              float64(0), // real Qdrant JSON round-trip shape
		qdrantChunkIDPayloadKey: semanticID,
	}

	ch := chunkFromMetadata(pointID, meta)
	if ch.ID != semanticID {
		t.Errorf("chunkFromMetadata ID = %q, want original semantic ID %q (not the Qdrant point ID %q)", ch.ID, semanticID, pointID)
	}
	if ch.Position != 0 {
		t.Errorf("chunkFromMetadata Position = %d, want 0 (float64 JSON round-trip must be handled)", ch.Position)
	}
	if ch.Content != "hello world" {
		t.Errorf("chunkFromMetadata Content = %q, want %q", ch.Content, "hello world")
	}
}

// TestChunkFromMetadata_NoStoredChunkID_FallsBackToPointID proves backward
// compatibility with points upserted before this fix landed (no chunk_id
// payload field) — Search still returns SOMETHING (the point ID) rather
// than an empty ID.
func TestChunkFromMetadata_NoStoredChunkID_FallsBackToPointID(t *testing.T) {
	pointID := uuid.NewString()
	meta := map[string]any{"content": "legacy point, no chunk_id field"}
	ch := chunkFromMetadata(pointID, meta)
	if ch.ID != pointID {
		t.Errorf("chunkFromMetadata ID = %q, want fallback to point ID %q", ch.ID, pointID)
	}
}
