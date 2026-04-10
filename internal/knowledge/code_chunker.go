package knowledge

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CodeAwareChunker splits documents along code structure boundaries such as
// function definitions, class definitions, and Markdown headers. When the
// document format is not recognized it falls back to fixed-size chunking with
// the configured overlap.
type CodeAwareChunker struct {
	maxChunkSize int
	overlap      int
}

// NewCodeAwareChunker returns a CodeAwareChunker with the given maximum chunk
// size and overlap. maxSize is clamped to a minimum of 1; overlap is clamped
// to [0, maxSize-1].
func NewCodeAwareChunker(maxSize, overlap int) *CodeAwareChunker {
	if maxSize < 1 {
		maxSize = 1
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= maxSize {
		overlap = maxSize - 1
	}
	return &CodeAwareChunker{maxChunkSize: maxSize, overlap: overlap}
}

// Chunk splits doc into chunks respecting code structure boundaries when the
// document source suggests a recognized language (Go, Python, Markdown). For
// unrecognized formats it delegates to FixedSizeChunker.
func (c *CodeAwareChunker) Chunk(doc Document) ([]Chunk, error) {
	if len([]rune(doc.Content)) == 0 {
		return nil, fmt.Errorf("document %q has no content", doc.ID)
	}

	ext := strings.ToLower(filepath.Ext(doc.Source))

	var boundaries []string
	switch ext {
	case ".go":
		boundaries = splitOnBoundaries(doc.Content, []string{"\nfunc "})
	case ".py":
		boundaries = splitOnBoundaries(doc.Content, []string{"\ndef ", "\nclass "})
	case ".md":
		boundaries = splitOnBoundaries(doc.Content, []string{"\n# "})
	default:
		return NewFixedSizeChunker(c.maxChunkSize, c.overlap).Chunk(doc)
	}

	return c.boundariesToChunks(doc, boundaries), nil
}

// splitOnBoundaries splits content by looking for any of the delimiter
// prefixes. Each split retains the delimiter at the start of the new section
// (except for the very first section which keeps whatever preceded the first
// delimiter). Leading newlines on delimiters are consumed from the previous
// section to keep boundaries clean.
func splitOnBoundaries(content string, delimiters []string) []string {
	// Collect all boundary positions.
	var hits []hit
	for _, d := range delimiters {
		start := 0
		for {
			idx := strings.Index(content[start:], d)
			if idx < 0 {
				break
			}
			// The delimiter starts with \n; the actual boundary position is
			// right after the newline so the delimiter keyword begins the new
			// section.
			pos := start + idx + 1 // skip the leading \n
			hits = append(hits, hit{pos: pos, delim: d})
			start = start + idx + len(d)
		}
	}

	if len(hits) == 0 {
		return []string{content}
	}

	// Sort by position.
	sortHits(hits)

	// Deduplicate overlapping positions.
	unique := []int{hits[0].pos}
	for i := 1; i < len(hits); i++ {
		if hits[i].pos != unique[len(unique)-1] {
			unique = append(unique, hits[i].pos)
		}
	}

	var sections []string
	prev := 0
	for _, pos := range unique {
		section := content[prev:pos]
		if strings.TrimSpace(section) != "" {
			sections = append(sections, section)
		}
		prev = pos
	}
	// Tail section.
	tail := content[prev:]
	if strings.TrimSpace(tail) != "" {
		sections = append(sections, tail)
	}

	if len(sections) == 0 {
		return []string{content}
	}
	return sections
}

// sortHits performs a simple insertion sort on boundary hits by position.
func sortHits(hits []hit) {
	for i := 1; i < len(hits); i++ {
		key := hits[i]
		j := i - 1
		for j >= 0 && hits[j].pos > key.pos {
			hits[j+1] = hits[j]
			j--
		}
		hits[j+1] = key
	}
}

// hit is an internal type used by splitOnBoundaries.
type hit struct {
	pos   int
	delim string
}

// boundariesToChunks converts boundary-split sections into Chunk values,
// further splitting any section that exceeds maxChunkSize using fixed-size
// chunking.
func (c *CodeAwareChunker) boundariesToChunks(doc Document, sections []string) []Chunk {
	var chunks []Chunk
	position := 0

	for _, section := range sections {
		if len([]rune(section)) <= c.maxChunkSize {
			meta := copyMeta(doc.Metadata)
			chunks = append(chunks, Chunk{
				ID:         fmt.Sprintf("%s-%d", doc.ID, position),
				DocumentID: doc.ID,
				Content:    section,
				Position:   position,
				Metadata:   meta,
			})
			position++
		} else {
			// Section exceeds max size — sub-chunk it.
			subDoc := Document{ID: doc.ID, Content: section, Metadata: doc.Metadata}
			subChunker := NewFixedSizeChunker(c.maxChunkSize, c.overlap)
			subChunks, err := subChunker.Chunk(subDoc)
			if err != nil {
				// Should not happen since we already checked content is non-empty.
				continue
			}
			for _, sc := range subChunks {
				sc.Position = position
				sc.ID = fmt.Sprintf("%s-%d", doc.ID, position)
				sc.DocumentID = doc.ID
				chunks = append(chunks, sc)
				position++
			}
		}
	}
	return chunks
}
