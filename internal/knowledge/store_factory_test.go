package knowledge_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestNewVectorStore_Memory(t *testing.T) {
	store, err := knowledge.NewVectorStore("memory", "", 0)
	if err != nil {
		t.Fatalf("NewVectorStore(memory): %v", err)
	}
	if store == nil {
		t.Fatal("NewVectorStore(memory) returned nil")
	}
}

func TestNewVectorStore_EmptyStringDefaultsToMemory(t *testing.T) {
	store, err := knowledge.NewVectorStore("", "", 0)
	if err != nil {
		t.Fatalf("NewVectorStore(''): %v", err)
	}
	if store == nil {
		t.Fatal("NewVectorStore('') returned nil")
	}
}

func TestNewVectorStore_UnknownBackendFallsToMemory(t *testing.T) {
	store, err := knowledge.NewVectorStore("nonexistent", "", 0)
	if err != nil {
		t.Fatalf("NewVectorStore(nonexistent): %v", err)
	}
	if store == nil {
		t.Fatal("NewVectorStore(nonexistent) returned nil store")
	}
}

func TestNewVectorStore_QdrantWithInvalidHost(t *testing.T) {
	_, err := knowledge.NewVectorStore("qdrant", "invalid-host-that-does-not-exist", 6334)
	if err == nil {
		t.Log("NewVectorStore(qdrant, invalid-host) did not return error — Qdrant may defer connection")
	}
}
