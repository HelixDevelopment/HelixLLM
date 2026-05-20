package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// AnalyzeCodeTool
// ---------------------------------------------------------------------------

// AnalyzeCodeTool computes basic code metrics for a directory or file.
type AnalyzeCodeTool struct {
	sandbox *Sandbox
}

// NewAnalyzeCodeTool creates an AnalyzeCodeTool.
func NewAnalyzeCodeTool(sandbox *Sandbox) *AnalyzeCodeTool {
	return &AnalyzeCodeTool{sandbox: sandbox}
}

func (a *AnalyzeCodeTool) Name() string { return "analyze_code" }
func (a *AnalyzeCodeTool) Description() string {
	return tr(keyAnalyzeCodeDesc)
}

func (a *AnalyzeCodeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyAnalyzeCodeParamPath),
			},
		},
		"required": []string{"path"},
	}
}

// funcPatterns matches common function declarations across languages.
var funcPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^func\s+`),                   // Go
	regexp.MustCompile(`^\s*def\s+\w+`),               // Python
	regexp.MustCompile(`^\s*function\s+\w+`),           // JavaScript
	regexp.MustCompile(`^\s*(public|private|protected|static|async)\s+.*\(`), // Java/TS
}

func (a *AnalyzeCodeTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("analyze_code: arguments must not be nil")
	}

	path, err := requireString(args, "path")
	if err != nil {
		return "", fmt.Errorf("analyze_code: %w", err)
	}

	if err := a.sandbox.ValidatePath(path); err != nil {
		return "", fmt.Errorf("analyze_code: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("analyze_code: %w", err)
	}

	var totalFiles, totalLines, totalFunctions int
	extensions := make(map[string]int)

	analyze := func(p string) {
		ext := filepath.Ext(p)
		if ext == "" {
			ext = "(no ext)"
		}
		extensions[ext]++
		totalFiles++

		f, err := os.Open(p)
		if err != nil {
			return
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			totalLines++
			line := scanner.Text()
			for _, pat := range funcPatterns {
				if pat.MatchString(line) {
					totalFunctions++
					break
				}
			}
		}
	}

	if info.IsDir() {
		// WalkDir reuses the fs.DirEntry produced by the directory read,
		// avoiding a per-entry os.Lstat on large trees.
		_ = filepath.WalkDir(path, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			// Skip binary and hidden files.
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return nil
			}
			fi, statErr := d.Info()
			if statErr != nil || fi.Size() > 2*1024*1024 {
				return nil
			}
			analyze(p)
			return nil
		})
	} else {
		analyze(path)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Files: %d\n", totalFiles)
	fmt.Fprintf(&sb, "Lines: %d\n", totalLines)
	fmt.Fprintf(&sb, "Functions: %d\n", totalFunctions)
	fmt.Fprintf(&sb, "\nBy extension:\n")
	for ext, count := range extensions {
		fmt.Fprintf(&sb, "  %s: %d files\n", ext, count)
	}

	return truncate(sb.String(), 10240), nil
}

// ---------------------------------------------------------------------------
// RunTestsTool
// ---------------------------------------------------------------------------

// RunTestsTool detects and runs test suites in a project directory.
type RunTestsTool struct {
	sandbox *Sandbox
}

// NewRunTestsTool creates a RunTestsTool.
func NewRunTestsTool(sandbox *Sandbox) *RunTestsTool {
	return &RunTestsTool{sandbox: sandbox}
}

func (r *RunTestsTool) Name() string { return "run_tests" }
func (r *RunTestsTool) Description() string {
	return tr(keyRunTestsDesc)
}

func (r *RunTestsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyRunTestsParamPath),
			},
			"framework": map[string]interface{}{
				"type":        "string",
				"description": tr(keyRunTestsParamFramework),
			},
		},
		"required": []string{},
	}
}

func (r *RunTestsTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	projectPath := optionalString(args, "path", ".")
	framework := optionalString(args, "framework", "")

	// Auto-detect framework if not specified.
	if framework == "" {
		framework = detectTestFramework(projectPath)
	}

	var out string
	var err error

	switch framework {
	case "go":
		out, err = r.sandbox.Execute(ctx, "go", "test", "-v", "-count=1", "-short", "./...")
	case "pytest":
		out, err = r.sandbox.Execute(ctx, "python3", "-m", "pytest", "-v", "--tb=short")
	case "jest":
		out, err = r.sandbox.Execute(ctx, "npx", "jest", "--verbose")
	case "npm":
		out, err = r.sandbox.Execute(ctx, "npm", "test")
	default:
		return "", fmt.Errorf("run_tests: could not detect test framework in %s", projectPath)
	}

	if err != nil {
		return "", fmt.Errorf("run_tests (%s): %w", framework, err)
	}
	return truncate(out, 10240), nil
}

// detectTestFramework inspects a directory for test framework indicators.
func detectTestFramework(path string) string {
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return "go"
	}
	if _, err := os.Stat(filepath.Join(path, "pytest.ini")); err == nil {
		return "pytest"
	}
	if _, err := os.Stat(filepath.Join(path, "setup.py")); err == nil {
		return "pytest"
	}
	if _, err := os.Stat(filepath.Join(path, "pyproject.toml")); err == nil {
		return "pytest"
	}
	if _, err := os.Stat(filepath.Join(path, "jest.config.js")); err == nil {
		return "jest"
	}
	if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
		return "npm"
	}
	return ""
}

// ---------------------------------------------------------------------------
// GetDependenciesTool
// ---------------------------------------------------------------------------

// GetDependenciesTool lists project dependencies by parsing manifest files.
type GetDependenciesTool struct {
	sandbox *Sandbox
}

// NewGetDependenciesTool creates a GetDependenciesTool.
func NewGetDependenciesTool(sandbox *Sandbox) *GetDependenciesTool {
	return &GetDependenciesTool{sandbox: sandbox}
}

func (g *GetDependenciesTool) Name() string { return "get_dependencies" }
func (g *GetDependenciesTool) Description() string {
	return tr(keyDepsDesc)
}

func (g *GetDependenciesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyDepsParamPath),
			},
		},
		"required": []string{"path"},
	}
}

func (g *GetDependenciesTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("get_dependencies: arguments must not be nil")
	}

	path, err := requireString(args, "path")
	if err != nil {
		return "", fmt.Errorf("get_dependencies: %w", err)
	}

	if err := g.sandbox.ValidatePath(path); err != nil {
		return "", fmt.Errorf("get_dependencies: %w", err)
	}

	// Try go.mod.
	goMod := filepath.Join(path, "go.mod")
	if data, err := os.ReadFile(goMod); err == nil {
		return truncate(fmt.Sprintf("go.mod dependencies:\n%s", parseDeps(string(data), "go")), 10240), nil
	}

	// Try package.json.
	pkgJSON := filepath.Join(path, "package.json")
	if data, err := os.ReadFile(pkgJSON); err == nil {
		return truncate(fmt.Sprintf("package.json:\n%s", string(data)), 10240), nil
	}

	// Try requirements.txt.
	reqTxt := filepath.Join(path, "requirements.txt")
	if data, err := os.ReadFile(reqTxt); err == nil {
		return truncate(fmt.Sprintf("requirements.txt:\n%s", string(data)), 10240), nil
	}

	return tr(keyDepsResultNoManifest), nil
}

// parseDeps extracts require blocks from Go module files.
func parseDeps(content, lang string) string {
	if lang != "go" {
		return content
	}

	var deps []string
	inRequire := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "require (" {
			inRequire = true
			continue
		}
		if trimmed == ")" {
			inRequire = false
			continue
		}
		if inRequire && trimmed != "" && !strings.HasPrefix(trimmed, "//") {
			deps = append(deps, trimmed)
		}
		// Single-line require.
		if strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "(") {
			deps = append(deps, strings.TrimPrefix(trimmed, "require "))
		}
	}

	if len(deps) == 0 {
		return "(no dependencies)"
	}
	return strings.Join(deps, "\n")
}

// ---------------------------------------------------------------------------
// CalculateComplexityTool
// ---------------------------------------------------------------------------

// CalculateComplexityTool estimates cyclomatic complexity by counting
// branching keywords per function.
type CalculateComplexityTool struct {
	sandbox *Sandbox
}

// NewCalculateComplexityTool creates a CalculateComplexityTool.
func NewCalculateComplexityTool(sandbox *Sandbox) *CalculateComplexityTool {
	return &CalculateComplexityTool{sandbox: sandbox}
}

func (c *CalculateComplexityTool) Name() string { return "calculate_complexity" }
func (c *CalculateComplexityTool) Description() string {
	return tr(keyComplexityDesc)
}

func (c *CalculateComplexityTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyComplexityParamPath),
			},
		},
		"required": []string{"path"},
	}
}

// branchKeywords are tokens that increase cyclomatic complexity.
var branchKeywords = []string{"if ", "for ", "switch ", "case ", "&&", "||"}

func (c *CalculateComplexityTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("calculate_complexity: arguments must not be nil")
	}

	path, err := requireString(args, "path")
	if err != nil {
		return "", fmt.Errorf("calculate_complexity: %w", err)
	}

	if err := c.sandbox.ValidatePath(path); err != nil {
		return "", fmt.Errorf("calculate_complexity: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("calculate_complexity: %w", err)
	}

	type funcComplexity struct {
		File       string
		Name       string
		Complexity int
	}
	var results []funcComplexity

	processFile := func(filePath string) {
		if filepath.Ext(filePath) != ".go" {
			return
		}
		f, err := os.Open(filePath)
		if err != nil {
			return
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		currentFunc := ""
		complexity := 1 // base complexity

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			// Detect function start.
			if strings.HasPrefix(line, "func ") {
				// Save previous function.
				if currentFunc != "" {
					results = append(results, funcComplexity{
						File: filePath, Name: currentFunc, Complexity: complexity,
					})
				}
				currentFunc = extractFuncName(line)
				complexity = 1
				continue
			}

			if currentFunc != "" {
				for _, kw := range branchKeywords {
					complexity += strings.Count(line, kw)
				}
			}
		}

		// Save last function.
		if currentFunc != "" {
			results = append(results, funcComplexity{
				File: filePath, Name: currentFunc, Complexity: complexity,
			})
		}
	}

	if info.IsDir() {
		// WalkDir reuses the fs.DirEntry produced by the directory read,
		// avoiding a per-entry os.Lstat on large trees.
		_ = filepath.WalkDir(path, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			processFile(p)
			return nil
		})
	} else {
		processFile(path)
	}

	if len(results) == 0 {
		return tr(keyComplexityResultNoFns), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Cyclomatic complexity analysis (%d functions):\n\n", len(results))
	for _, r := range results {
		label := "LOW"
		if r.Complexity > 10 {
			label = "HIGH"
		} else if r.Complexity > 5 {
			label = "MEDIUM"
		}
		fmt.Fprintf(&sb, "  %s: %d (%s) [%s]\n", r.Name, r.Complexity, label, r.File)
	}

	return truncate(sb.String(), 10240), nil
}

// extractFuncName pulls the function name from a func declaration line.
func extractFuncName(line string) string {
	// Handle method receiver: func (r *Foo) Bar(...)
	line = strings.TrimPrefix(line, "func ")
	if strings.HasPrefix(line, "(") {
		// Skip past the receiver.
		idx := strings.Index(line, ")")
		if idx >= 0 {
			line = strings.TrimSpace(line[idx+1:])
		}
	}
	// Take everything up to the first '('.
	if idx := strings.Index(line, "("); idx >= 0 {
		return line[:idx]
	}
	return line
}
