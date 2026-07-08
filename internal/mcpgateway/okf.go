package mcpgateway

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// OKFResource describes one Open Knowledge Format concept file (a Markdown
// file with YAML frontmatter) discovered under an OKF root directory, per
// the MCP_OKF_GATEWAY_MEMO.md §2.2 integration shape: OKF is content served
// through MCP's `resources` capability, never a new wire protocol.
type OKFResource struct {
	// URI is the MCP resource URI, form "okf://<relative-path>".
	URI string
	// Name is the file's base name (without extension).
	Name string
	// Path is the absolute filesystem path backing this resource.
	Path string
}

// OKFStore lists and reads Markdown concept files under Root as MCP
// resources. Root is config-injected (CONST-045/046) — never hardcoded.
// A missing/empty Root is not an error: it simply yields zero resources
// (an OKF store is optional groundwork per the memo, §2.2 item 3).
type OKFStore struct {
	Root string
}

// NewOKFStore constructs a store rooted at root. An empty root disables
// OKF resource listing (List/Read return empty/not-found respectively)
// rather than defaulting to any implicit path (§11.4.6 — no guessing).
func NewOKFStore(root string) *OKFStore {
	return &OKFStore{Root: root}
}

// List enumerates every *.md file under Root, deterministically sorted by
// relative path, as OKF resources. Read-only — never writes into Root.
func (s *OKFStore) List() ([]OKFResource, error) {
	if s.Root == "" {
		return nil, nil
	}
	info, err := os.Stat(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat OKF root %q: %w", s.Root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("OKF root %q is not a directory", s.Root)
	}

	var out []OKFResource
	err = filepath.WalkDir(s.Root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(s.Root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		out = append(out, OKFResource{
			URI:  "okf://" + rel,
			Name: base,
			Path: path,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking OKF root %q: %w", s.Root, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out, nil
}

// Read returns the raw Markdown content (frontmatter + body) for the OKF
// resource identified by uri (form "okf://<relative-path>"), performing a
// REAL filesystem read — never a canned/simulated body.
func (s *OKFStore) Read(uri string) (string, error) {
	rel := strings.TrimPrefix(uri, "okf://")
	if rel == uri || rel == "" {
		return "", fmt.Errorf("invalid OKF resource uri %q", uri)
	}
	// Reject escapes outside Root (path traversal).
	cleanRel := filepath.Clean(rel)
	if strings.HasPrefix(cleanRel, "..") {
		return "", fmt.Errorf("OKF resource uri %q escapes root", uri)
	}
	full := filepath.Join(s.Root, cleanRel)
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("reading OKF resource %q: %w", uri, err)
	}
	return string(data), nil
}
