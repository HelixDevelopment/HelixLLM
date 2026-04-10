package knowledge

// MMRRerank applies Maximal Marginal Relevance re-ranking to reduce redundancy
// in a set of scored chunks. lambda controls the trade-off between relevance
// and diversity: 1.0 means pure relevance, 0.0 means pure diversity. A
// typical value is 0.5-0.7. finalK is the number of chunks to return.
//
// The algorithm greedily selects the chunk that maximizes:
//
//	lambda * relevance(chunk) - (1-lambda) * max_similarity(chunk, selected)
//
// Chunks must have pre-computed embeddings. Chunks without embeddings are
// appended at the end (preserving their original order) after the MMR
// selection.
func MMRRerank(chunks []ScoredChunk, lambda float64, finalK int) []ScoredChunk {
	if len(chunks) == 0 || finalK <= 0 {
		return nil
	}
	if finalK > len(chunks) {
		finalK = len(chunks)
	}
	if lambda < 0 {
		lambda = 0
	}
	if lambda > 1 {
		lambda = 1
	}

	// Separate chunks with and without embeddings.
	var withEmb []ScoredChunk
	var withoutEmb []ScoredChunk
	for _, c := range chunks {
		if len(c.Embedding) > 0 {
			withEmb = append(withEmb, c)
		} else {
			withoutEmb = append(withoutEmb, c)
		}
	}

	// Normalize relevance scores to [0, 1] for fair comparison with cosine
	// similarity which is also in [-1, 1] (but typically [0, 1] for
	// normalized vectors).
	maxRel := 0.0
	for _, c := range withEmb {
		if c.Score > maxRel {
			maxRel = c.Score
		}
	}
	if maxRel == 0 {
		maxRel = 1.0
	}

	// Track which candidates have been selected.
	selected := make([]ScoredChunk, 0, finalK)
	used := make(map[int]bool, len(withEmb))

	for len(selected) < finalK && len(selected) < len(withEmb) {
		bestIdx := -1
		bestMMR := -1e18

		for i, candidate := range withEmb {
			if used[i] {
				continue
			}

			relevance := candidate.Score / maxRel

			// Compute max cosine similarity to any already-selected chunk.
			maxSim := 0.0
			for _, sel := range selected {
				sim := cosineSimilarity(candidate.Embedding, sel.Embedding)
				if sim > maxSim {
					maxSim = sim
				}
			}

			mmrScore := lambda*relevance - (1-lambda)*maxSim

			if mmrScore > bestMMR {
				bestMMR = mmrScore
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break
		}

		used[bestIdx] = true
		chosen := withEmb[bestIdx]
		// Store the MMR score as the final score so callers can see the
		// re-ranked ordering.
		chosen.Score = bestMMR
		selected = append(selected, chosen)
	}

	// Fill remaining slots with chunks that had no embeddings.
	for _, c := range withoutEmb {
		if len(selected) >= finalK {
			break
		}
		selected = append(selected, c)
	}

	return selected
}
