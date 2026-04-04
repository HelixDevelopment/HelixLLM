package knowledge

import "fmt"

// NewVectorStore constructs a VectorStore by backend name.
//
// Supported backends:
//   - "memory"  → in-process MemoryStore (no external dependency)
//   - "qdrant"  → QdrantStore connecting to host:port
//
// Any unrecognised backend name falls back to MemoryStore so the server
// starts cleanly even when the vector DB container is not yet available.
func NewVectorStore(backend, host string, port int) (VectorStore, error) {
	switch backend {
	case "qdrant":
		s, err := NewQdrantStore(host, port)
		if err != nil {
			return nil, fmt.Errorf("store_factory: qdrant: %w", err)
		}
		return s, nil
	case "memory", "":
		return NewMemoryStore(), nil
	default:
		return NewMemoryStore(), nil
	}
}
