package knowledge_test

// DZ-05 regression guard — knowledge internal API MUST require auth.
// §11.4.115 polarity switch (RED_MODE, default "1"): RED_MODE=1 reproduces the
// pre-fix exposure (unauth reaches /internal/knowledge/*), RED_MODE=0 is the
// standing GREEN guard (unauth -> 401, valid Bearer -> not 401).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	gwmw "github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestDZ05_KnowledgeRequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const key = "sk-dz05-knowledge"
	red := os.Getenv("RED_MODE") != "0"

	r := gin.New()
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder: knowledge.NewHashEmbedder(768),
		Store:    knowledge.NewMemoryStore(),
		Chunker:  knowledge.NewFixedSizeChunker(512, 64),
	})
	if red {
		knowledge.RegisterKnowledgeRoutes(r, pipeline)
	} else {
		knowledge.RegisterKnowledgeRoutes(r, pipeline, gwmw.APIKeyAuth(key))
	}

	unauth := httptest.NewRecorder()
	r.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/internal/knowledge/stats", nil))

	if red {
		if unauth.Code == http.StatusUnauthorized {
			t.Fatalf("RED expected exposure (unauth reachable) but got 401")
		}
		t.Logf("DZ-05 RED reproduced: unauth GET /internal/knowledge/stats -> HTTP %d", unauth.Code)
		return
	}

	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("GREEN expected 401 for unauthenticated request, got %d", unauth.Code)
	}
	authed := httptest.NewRecorder()
	authReq := httptest.NewRequest(http.MethodGet, "/internal/knowledge/stats", nil)
	authReq.Header.Set("Authorization", "Bearer "+key)
	r.ServeHTTP(authed, authReq)
	if authed.Code == http.StatusUnauthorized {
		t.Fatalf("GREEN authenticated request wrongly rejected with 401")
	}
	t.Logf("DZ-05 GREEN: unauth -> 401, auth -> HTTP %d", authed.Code)
}
