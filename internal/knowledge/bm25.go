package knowledge

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// tokenPattern matches sequences of alphanumeric characters for BM25
// tokenization.
var tokenPattern = regexp.MustCompile(`[a-zA-Z0-9]+`)

// BM25Index is a keyword search index implementing the Okapi BM25 ranking
// function. It is safe for concurrent use.
type BM25Index struct {
	mu        sync.RWMutex
	documents map[string][]string // chunk ID → tokenized words
	contents  map[string]string   // chunk ID → original text
	docFreqs  map[string]int      // term → document count
	avgDocLen float64
	totalDocs int
	k1, b     float64 // BM25 parameters
}

// NewBM25Index returns a new empty BM25Index with standard parameters
// k1=1.5 and b=0.75.
func NewBM25Index() *BM25Index {
	return &BM25Index{
		documents: make(map[string][]string),
		contents:  make(map[string]string),
		docFreqs:  make(map[string]int),
		k1:        1.5,
		b:         0.75,
	}
}

// Add tokenizes text and indexes it under the given chunk ID. If the ID
// already exists it is replaced.
func (idx *BM25Index) Add(id string, text string) {
	tokens := tokenize(text)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// If the ID already exists, remove its term contributions first.
	if old, exists := idx.documents[id]; exists {
		seen := uniqueTerms(old)
		for term := range seen {
			idx.docFreqs[term]--
			if idx.docFreqs[term] <= 0 {
				delete(idx.docFreqs, term)
			}
		}
		idx.totalDocs--
	}

	idx.documents[id] = tokens
	idx.contents[id] = text
	idx.totalDocs++

	// Update document frequencies.
	seen := uniqueTerms(tokens)
	for term := range seen {
		idx.docFreqs[term]++
	}

	// Recompute average document length.
	idx.recomputeAvgDocLen()
}

// Search scores every indexed document against the query using BM25 and
// returns the top K results sorted by score descending.
func (idx *BM25Index) Search(query string, topK int) []ScoredChunk {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.totalDocs == 0 {
		return nil
	}

	type scored struct {
		id    string
		score float64
	}

	var results []scored
	n := float64(idx.totalDocs)

	for id, docTokens := range idx.documents {
		docLen := float64(len(docTokens))
		tf := termFrequencies(docTokens)
		var score float64

		for _, term := range queryTokens {
			termTF, ok := tf[term]
			if !ok {
				continue
			}

			df := float64(idx.docFreqs[term])
			// IDF = log((N - df + 0.5) / (df + 0.5) + 1)
			idf := math.Log((n-df+0.5)/(df+0.5) + 1.0)

			// BM25 term score
			tfFloat := float64(termTF)
			denom := tfFloat + idx.k1*(1.0-idx.b+idx.b*docLen/idx.avgDocLen)
			termScore := idf * (tfFloat * (idx.k1 + 1.0)) / denom

			score += termScore
		}

		if score > 0 {
			results = append(results, scored{id: id, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}

	chunks := make([]ScoredChunk, len(results))
	for i, r := range results {
		chunks[i] = ScoredChunk{
			Chunk: Chunk{
				ID:      r.id,
				Content: idx.contents[r.id],
			},
			Score: r.score,
		}
	}
	return chunks
}

// Remove deletes a document from the index by chunk ID.
func (idx *BM25Index) Remove(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	tokens, exists := idx.documents[id]
	if !exists {
		return
	}

	seen := uniqueTerms(tokens)
	for term := range seen {
		idx.docFreqs[term]--
		if idx.docFreqs[term] <= 0 {
			delete(idx.docFreqs, term)
		}
	}

	delete(idx.documents, id)
	delete(idx.contents, id)
	idx.totalDocs--

	idx.recomputeAvgDocLen()
}

// recomputeAvgDocLen recalculates the average document length across all
// indexed documents. Must be called with the write lock held.
func (idx *BM25Index) recomputeAvgDocLen() {
	if idx.totalDocs == 0 {
		idx.avgDocLen = 0
		return
	}
	total := 0
	for _, tokens := range idx.documents {
		total += len(tokens)
	}
	idx.avgDocLen = float64(total) / float64(idx.totalDocs)
}

// tokenize splits text into lowercase alphanumeric tokens.
func tokenize(text string) []string {
	matches := tokenPattern.FindAllString(strings.ToLower(text), -1)
	return matches
}

// termFrequencies counts the number of occurrences of each term in tokens.
func termFrequencies(tokens []string) map[string]int {
	tf := make(map[string]int, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}
	return tf
}

// uniqueTerms returns a set of distinct terms present in tokens.
func uniqueTerms(tokens []string) map[string]struct{} {
	seen := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		seen[t] = struct{}{}
	}
	return seen
}
