package brain

import (
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ComplexityLevel classifies a request's computational complexity.
type ComplexityLevel string

const (
	// ComplexitySimple maps to TierFast (score 0-2).
	ComplexitySimple ComplexityLevel = "simple"
	// ComplexityModerate maps to TierBalanced (score 3-5).
	ComplexityModerate ComplexityLevel = "moderate"
	// ComplexityComplex maps to TierPowerful (score 6+).
	ComplexityComplex ComplexityLevel = "complex"
)

// ComplexityResult carries the full analysis outcome.
type ComplexityResult struct {
	Level         ComplexityLevel  `json:"level"`
	Score         int              `json:"score"`
	Reasons       []string         `json:"reasons"`
	TargetTier    models.ModelTier `json:"target_tier"`
	ModelOverride bool             `json:"model_override"`
}

// ComplexityAnalyzer inspects an InternalChatRequest and produces a
// ComplexityResult that the fleet router can use for tier selection.
type ComplexityAnalyzer struct {
	complexKeywords []string
}

// NewComplexityAnalyzer returns a ComplexityAnalyzer seeded with the default
// set of complexity-indicating keywords.
func NewComplexityAnalyzer() *ComplexityAnalyzer {
	return &ComplexityAnalyzer{
		complexKeywords: []string{
			"analyze",
			"compare",
			"explain",
			"refactor",
			"architect",
			"investigate",
			"comprehensive",
			"thorough",
		},
	}
}

// Analyze scores the request and returns a ComplexityResult.
//
// Scoring rules (cumulative):
//   - req.Model != ""         → early return with ModelOverride=true
//   - tools > 3               → +2
//   - tools 1-3               → +1
//   - last user msg > 2000 ch → +2
//   - last user msg > 500 ch  → +1
//   - each complexity keyword (case-insensitive, max 3 matches) → +1
//   - messages > 5            → +1
//   - any system msg > 1000 ch → +1
//
// Score bands: 0-2 → simple/TierFast, 3-5 → moderate/TierBalanced, 6+ → complex/TierPowerful.
func (a *ComplexityAnalyzer) Analyze(req *types.InternalChatRequest) ComplexityResult {
	// User explicitly chose a model — honour it without scoring.
	if req.Model != "" {
		return ComplexityResult{
			ModelOverride: true,
		}
	}

	score := 0
	var reasons []string

	// Tool count.
	toolCount := len(req.Tools)
	switch {
	case toolCount > 3:
		score += 2
		reasons = append(reasons, "many tools (>3)")
	case toolCount >= 1:
		score += 1
		reasons = append(reasons, "has tools (1-3)")
	}

	// Last user message length.
	if content := lastUserContent(req.Messages); content != "" {
		switch {
		case len(content) > 2000:
			score += 2
			reasons = append(reasons, "long user message (>2000 chars)")
		case len(content) > 500:
			score += 1
			reasons = append(reasons, "medium user message (>500 chars)")
		}

		// Keyword matching (case-insensitive, capped at 3).
		lower := strings.ToLower(content)
		matched := 0
		for _, kw := range a.complexKeywords {
			if matched >= 3 {
				break
			}
			if strings.Contains(lower, kw) {
				score++
				matched++
				reasons = append(reasons, "keyword: "+kw)
			}
		}
	}

	// Long conversation.
	if len(req.Messages) > 5 {
		score++
		reasons = append(reasons, "long conversation (>5 messages)")
	}

	// Large system prompt.
	for _, msg := range req.Messages {
		if msg.Role == types.RoleSystem && len(msg.Content) > 1000 {
			score++
			reasons = append(reasons, "large system prompt (>1000 chars)")
			break // count once
		}
	}

	return ComplexityResult{
		Level:      levelFromScore(score),
		Score:      score,
		Reasons:    reasons,
		TargetTier: tierFromScore(score),
	}
}

// lastUserContent returns the Content of the last message whose Role is
// RoleUser. Returns "" when no such message exists.
func lastUserContent(msgs []types.InternalMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

func levelFromScore(score int) ComplexityLevel {
	switch {
	case score >= 6:
		return ComplexityComplex
	case score >= 3:
		return ComplexityModerate
	default:
		return ComplexitySimple
	}
}

func tierFromScore(score int) models.ModelTier {
	switch {
	case score >= 6:
		return models.TierPowerful
	case score >= 3:
		return models.TierBalanced
	default:
		return models.TierFast
	}
}
