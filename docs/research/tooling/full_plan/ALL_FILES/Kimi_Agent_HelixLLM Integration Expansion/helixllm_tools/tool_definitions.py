"""
HelixLLM Tool Definitions for Coding Tasks
===========================================
Complete tool definitions for software development workflows.

Tools Included:
- File System: read_file, write_file, list_directory, search_files
- Code Execution: execute_python, execute_shell
- Git Operations: git_status, git_diff, git_log
- Testing: run_tests, analyze_test_results
- Analysis: analyze_code, get_dependencies, calculate_complexity
"""

from typing import Dict, List, Any, Optional
from tool_registry import (
    ToolDefinition, ParameterSchema, ToolCategory, 
    ToolPermission, get_registry
)


def create_file_system_tools() -> List[ToolDefinition]:
    """Create file system operation tools."""
    
    return [
        ToolDefinition(
            name="read_file",
            description="Read the contents of a file at the specified path. Returns file content as string. Use limit parameter for large files.",
            category=ToolCategory.FILE_SYSTEM,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Absolute path to the file to read (e.g., /home/user/project/main.py)",
                    required=True
                ),
                ParameterSchema(
                    name="offset",
                    type="integer",
                    description="Line number to start reading from (1-based index)",
                    required=False,
                    default=1
                ),
                ParameterSchema(
                    name="limit",
                    type="integer",
                    description="Maximum number of lines to read. Use 100 for normal files, 500 for large files.",
                    required=False,
                    default=100
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["file", "read", "content"],
            examples=[
                {"arguments": {"path": "/home/user/project/main.py"}},
                {"arguments": {"path": "/home/user/project/main.py", "offset": 50, "limit": 30}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "content": {"type": "string"},
                    "lines_read": {"type": "integer"},
                    "total_lines": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                }
            }
        ),
        
        ToolDefinition(
            name="write_file",
            description="Write content to a file. Creates file if it doesn't exist, overwrites if it does. Use append=True to append content.",
            category=ToolCategory.FILE_SYSTEM,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Absolute path where to write the file",
                    required=True
                ),
                ParameterSchema(
                    name="content",
                    type="string",
                    description="Content to write to the file",
                    required=True
                ),
                ParameterSchema(
                    name="append",
                    type="boolean",
                    description="If true, append to existing file instead of overwriting",
                    required=False,
                    default=False
                )
            ],
            permissions=[ToolPermission.WRITE, ToolPermission.DESTRUCTIVE],
            tags=["file", "write", "create"],
            examples=[
                {"arguments": {"path": "/home/user/project/main.py", "content": "print('hello')"}},
                {"arguments": {"path": "/home/user/project/log.txt", "content": "New log entry\n", "append": True}}
            ],
            requires_confirmation=True,
            returns={
                "type": "object",
                "properties": {
                    "success": {"type": "boolean"},
                    "bytes_written": {"type": "integer"},
                    "path": {"type": "string"}
                }
            }
        ),
        
        ToolDefinition(
            name="list_directory",
            description="List contents of a directory with file information. Returns files and subdirectories with metadata.",
            category=ToolCategory.FILE_SYSTEM,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Absolute path to the directory to list",
                    required=True
                ),
                ParameterSchema(
                    name="recursive",
                    type="boolean",
                    description="If true, list recursively including subdirectories",
                    required=False,
                    default=False
                ),
                ParameterSchema(
                    name="show_hidden",
                    type="boolean",
                    description="If true, include hidden files (starting with .)",
                    required=False,
                    default=False
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["directory", "list", "browse"],
            examples=[
                {"arguments": {"path": "/home/user/project"}},
                {"arguments": {"path": "/home/user/project", "recursive": True}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "path": {"type": "string"},
                    "entries": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "properties": {
                                "name": {"type": "string"},
                                "type": {"type": "string", "enum": ["file", "directory"]},
                                "size": {"type": "integer"},
                                "modified": {"type": "string"}
                            }
                        }
                    }
                }
            }
        ),
        
        ToolDefinition(
            name="search_files",
            description="Search for files or content within files. Supports glob patterns and content search.",
            category=ToolCategory.FILE_SYSTEM,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Directory path to search in",
                    required=True
                ),
                ParameterSchema(
                    name="pattern",
                    type="string",
                    description="Search pattern - can be glob pattern (e.g., '*.py') or content pattern",
                    required=True
                ),
                ParameterSchema(
                    name="search_content",
                    type="boolean",
                    description="If true, search file contents instead of filenames",
                    required=False,
                    default=False
                ),
                ParameterSchema(
                    name="file_pattern",
                    type="string",
                    description="When search_content=True, only search in files matching this glob pattern",
                    required=False,
                    default="*"
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["search", "find", "grep"],
            examples=[
                {"arguments": {"path": "/home/user/project", "pattern": "*.py"}},
                {"arguments": {"path": "/home/user/project", "pattern": "TODO", "search_content": True}},
                {"arguments": {"path": "/home/user/project", "pattern": "function", "search_content": True, "file_pattern": "*.js"}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "matches": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "properties": {
                                "path": {"type": "string"},
                                "line": {"type": "integer"},
                                "content": {"type": "string"}
                            }
                        }
                    },
                    "total_matches": {"type": "integer"}
                }
            }
        ),
        
        ToolDefinition(
            name="file_exists",
            description="Check if a file or directory exists at the specified path.",
            category=ToolCategory.FILE_SYSTEM,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Path to check",
                    required=True
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["file", "check", "exists"],
            examples=[
                {"arguments": {"path": "/home/user/project/main.py"}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "exists": {"type": "boolean"},
                    "type": {"type": "string", "enum": ["file", "directory", "none"]}
                }
            }
        )
    ]


def create_code_execution_tools() -> List[ToolDefinition]:
    """Create code execution tools."""
    
    return [
        ToolDefinition(
            name="execute_python",
            description="Execute Python code in a sandboxed environment. Returns stdout, stderr, and any return value. Use for data analysis, calculations, and scripting.",
            category=ToolCategory.CODE_EXECUTION,
            parameters=[
                ParameterSchema(
                    name="code",
                    type="string",
                    description="Python code to execute. Can include multiple lines, imports, and function definitions.",
                    required=True
                ),
                ParameterSchema(
                    name="timeout",
                    type="integer",
                    description="Maximum execution time in seconds",
                    required=False,
                    default=30
                ),
                ParameterSchema(
                    name="restart",
                    type="boolean",
                    description="If true, restart the Python environment before execution (clears variables)",
                    required=False,
                    default=False
                )
            ],
            permissions=[ToolPermission.EXECUTE],
            tags=["python", "code", "execute", "script"],
            examples=[
                {"arguments": {"code": "print('Hello, World!')"}},
                {"arguments": {"code": "import math\nresult = math.sqrt(16)\nprint(result)"}},
                {"arguments": {"code": "import pandas as pd\ndf = pd.DataFrame({'a': [1, 2, 3]})\nprint(df)"}}
            ],
            timeout_seconds=60,
            returns={
                "type": "object",
                "properties": {
                    "stdout": {"type": "string"},
                    "stderr": {"type": "string"},
                    "result": {"type": "any"},
                    "execution_time": {"type": "number"}
                }
            }
        ),
        
        ToolDefinition(
            name="execute_shell",
            description="Execute a shell command. Use for system operations, file manipulation, and running external tools. Be careful with destructive commands.",
            category=ToolCategory.CODE_EXECUTION,
            parameters=[
                ParameterSchema(
                    name="command",
                    type="string",
                    description="Shell command to execute. Supports pipes, redirections, and most shell features.",
                    required=True
                ),
                ParameterSchema(
                    name="timeout",
                    type="integer",
                    description="Maximum execution time in seconds",
                    required=False,
                    default=30
                ),
                ParameterSchema(
                    name="description",
                    type="string",
                    description="Brief description of what the command does (for logging)",
                    required=False,
                    default=""
                )
            ],
            permissions=[ToolPermission.EXECUTE, ToolPermission.DESTRUCTIVE],
            tags=["shell", "bash", "command", "system"],
            examples=[
                {"arguments": {"command": "ls -la", "description": "List files in current directory"}},
                {"arguments": {"command": "find . -name '*.py' | head -20", "description": "Find Python files"}},
                {"arguments": {"command": "wc -l src/*.py", "description": "Count lines in Python files"}}
            ],
            timeout_seconds=120,
            requires_confirmation=True,
            returns={
                "type": "object",
                "properties": {
                    "stdout": {"type": "string"},
                    "stderr": {"type": "string"},
                    "exit_code": {"type": "integer"},
                    "execution_time": {"type": "number"}
                }
            }
        )
    ]


def create_git_tools() -> List[ToolDefinition]:
    """Create Git operation tools."""
    
    return [
        ToolDefinition(
            name="git_status",
            description="Get the current git repository status including modified files, staged changes, and branch information.",
            category=ToolCategory.GIT,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Path to the git repository (or subdirectory within it)",
                    required=True
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["git", "status", "vcs"],
            examples=[
                {"arguments": {"path": "/home/user/project"}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "branch": {"type": "string"},
                    "modified": {"type": "array", "items": {"type": "string"}},
                    "staged": {"type": "array", "items": {"type": "string"}},
                    "untracked": {"type": "array", "items": {"type": "string"}},
                    "ahead": {"type": "integer"},
                    "behind": {"type": "integer"}
                }
            }
        ),
        
        ToolDefinition(
            name="git_diff",
            description="Show changes between commits, branches, or working directory. Can show diff for specific files.",
            category=ToolCategory.GIT,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Path to the git repository",
                    required=True
                ),
                ParameterSchema(
                    name="target",
                    type="string",
                    description="Target to diff against (commit hash, branch name, or 'HEAD'). Use empty for unstaged changes.",
                    required=False,
                    default=""
                ),
                ParameterSchema(
                    name="file",
                    type="string",
                    description="Specific file to show diff for",
                    required=False,
                    default=""
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["git", "diff", "changes"],
            examples=[
                {"arguments": {"path": "/home/user/project"}},
                {"arguments": {"path": "/home/user/project", "target": "HEAD~1"}},
                {"arguments": {"path": "/home/user/project", "file": "main.py"}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "diff": {"type": "string"},
                    "files_changed": {"type": "array", "items": {"type": "string"}}
                }
            }
        ),
        
        ToolDefinition(
            name="git_log",
            description="Show commit history with optional filtering. Returns commits with hash, author, date, and message.",
            category=ToolCategory.GIT,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Path to the git repository",
                    required=True
                ),
                ParameterSchema(
                    name="limit",
                    type="integer",
                    description="Maximum number of commits to show",
                    required=False,
                    default=10
                ),
                ParameterSchema(
                    name="file",
                    type="string",
                    description="Only show commits affecting this file",
                    required=False,
                    default=""
                ),
                ParameterSchema(
                    name="author",
                    type="string",
                    description="Filter by author name or email",
                    required=False,
                    default=""
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["git", "log", "history"],
            examples=[
                {"arguments": {"path": "/home/user/project", "limit": 5}},
                {"arguments": {"path": "/home/user/project", "file": "main.py", "limit": 20}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "commits": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "properties": {
                                "hash": {"type": "string"},
                                "author": {"type": "string"},
                                "date": {"type": "string"},
                                "message": {"type": "string"}
                            }
                        }
                    }
                }
            }
        ),
        
        ToolDefinition(
            name="git_branch",
            description="List branches or get current branch information.",
            category=ToolCategory.GIT,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Path to the git repository",
                    required=True
                ),
                ParameterSchema(
                    name="all",
                    type="boolean",
                    description="Include remote branches",
                    required=False,
                    default=False
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["git", "branch"],
            examples=[
                {"arguments": {"path": "/home/user/project"}},
                {"arguments": {"path": "/home/user/project", "all": True}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "current": {"type": "string"},
                    "branches": {"type": "array", "items": {"type": "string"}}
                }
            }
        )
    ]


def create_testing_tools() -> List[ToolDefinition]:
    """Create testing and quality tools."""
    
    return [
        ToolDefinition(
            name="run_tests",
            description="Run the test suite for a project. Supports pytest, unittest, and other test frameworks.",
            category=ToolCategory.TESTING,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Path to the project or test directory",
                    required=True
                ),
                ParameterSchema(
                    name="test_path",
                    type="string",
                    description="Specific test file or test to run (e.g., 'tests/test_main.py::test_function')",
                    required=False,
                    default=""
                ),
                ParameterSchema(
                    name="framework",
                    type="string",
                    description="Test framework to use",
                    required=False,
                    default="auto",
                    enum=["auto", "pytest", "unittest", "jest", "mocha"]
                ),
                ParameterSchema(
                    name="verbose",
                    type="boolean",
                    description="Show detailed test output",
                    required=False,
                    default=True
                )
            ],
            permissions=[ToolPermission.EXECUTE],
            tags=["test", "pytest", "unittest", "quality"],
            examples=[
                {"arguments": {"path": "/home/user/project"}},
                {"arguments": {"path": "/home/user/project", "test_path": "tests/test_main.py"}},
                {"arguments": {"path": "/home/user/project", "framework": "pytest", "verbose": True}}
            ],
            timeout_seconds=300,
            returns={
                "type": "object",
                "properties": {
                    "passed": {"type": "integer"},
                    "failed": {"type": "integer"},
                    "skipped": {"type": "integer"},
                    "duration": {"type": "number"},
                    "failures": {"type": "array", "items": {"type": "object"}}
                }
            }
        ),
        
        ToolDefinition(
            name="analyze_code",
            description="Perform static code analysis on a file or directory. Returns issues, metrics, and suggestions.",
            category=ToolCategory.ANALYSIS,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Path to file or directory to analyze",
                    required=True
                ),
                ParameterSchema(
                    name="language",
                    type="string",
                    description="Programming language (auto-detected if not specified)",
                    required=False,
                    default="auto",
                    enum=["auto", "python", "javascript", "typescript", "java", "go", "rust"]
                ),
                ParameterSchema(
                    name="include_metrics",
                    type="boolean",
                    description="Include code complexity and quality metrics",
                    required=False,
                    default=True
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["analysis", "lint", "quality", "metrics"],
            examples=[
                {"arguments": {"path": "/home/user/project/src"}},
                {"arguments": {"path": "/home/user/project/main.py", "language": "python"}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "issues": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "properties": {
                                "severity": {"type": "string"},
                                "message": {"type": "string"},
                                "line": {"type": "integer"},
                                "file": {"type": "string"}
                            }
                        }
                    },
                    "metrics": {"type": "object"}
                }
            }
        ),
        
        ToolDefinition(
            name="get_dependencies",
            description="Extract and analyze project dependencies from package files (requirements.txt, package.json, etc.)",
            category=ToolCategory.ANALYSIS,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Path to project root or dependency file",
                    required=True
                ),
                ParameterSchema(
                    name="format",
                    type="string",
                    description="Output format",
                    required=False,
                    default="list",
                    enum=["list", "tree", "graph"]
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["dependencies", "packages", "analysis"],
            examples=[
                {"arguments": {"path": "/home/user/project"}},
                {"arguments": {"path": "/home/user/project/requirements.txt"}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "dependencies": {"type": "array"},
                    "dev_dependencies": {"type": "array"},
                    "outdated": {"type": "array"}
                }
            }
        ),
        
        ToolDefinition(
            name="calculate_complexity",
            description="Calculate cyclomatic complexity and other code complexity metrics for a file or directory.",
            category=ToolCategory.ANALYSIS,
            parameters=[
                ParameterSchema(
                    name="path",
                    type="string",
                    description="Path to file or directory",
                    required=True
                ),
                ParameterSchema(
                    name="threshold",
                    type="integer",
                    description="Complexity threshold for flagging functions (default: 10)",
                    required=False,
                    default=10
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["complexity", "metrics", "analysis"],
            examples=[
                {"arguments": {"path": "/home/user/project/src"}},
                {"arguments": {"path": "/home/user/project/main.py", "threshold": 15}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "average_complexity": {"type": "number"},
                    "max_complexity": {"type": "number"},
                    "functions": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "properties": {
                                "name": {"type": "string"},
                                "complexity": {"type": "integer"},
                                "line": {"type": "integer"}
                            }
                        }
                    }
                }
            }
        )
    ]


def create_web_tools() -> List[ToolDefinition]:
    """Create web-related tools."""
    
    return [
        ToolDefinition(
            name="web_search",
            description="Search the web for information. Returns search results with titles, snippets, and URLs.",
            category=ToolCategory.WEB,
            parameters=[
                ParameterSchema(
                    name="queries",
                    type="array",
                    description="List of search queries to execute",
                    required=True,
                    items={"type": "string"}
                ),
                ParameterSchema(
                    name="total_count",
                    type="integer",
                    description="Maximum total results to return",
                    required=False,
                    default=10
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["web", "search", "internet"],
            examples=[
                {"arguments": {"queries": ["python async tutorial"], "total_count": 5}},
                {"arguments": {"queries": ["fastapi best practices", "pydantic validation"], "total_count": 10}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "results": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "properties": {
                                "title": {"type": "string"},
                                "snippet": {"type": "string"},
                                "url": {"type": "string"}
                            }
                        }
                    }
                }
            }
        ),
        
        ToolDefinition(
            name="fetch_url",
            description="Fetch content from a URL. Returns page content and metadata.",
            category=ToolCategory.WEB,
            parameters=[
                ParameterSchema(
                    name="url",
                    type="string",
                    description="URL to fetch",
                    required=True
                ),
                ParameterSchema(
                    name="extract_text",
                    type="boolean",
                    description="Extract main text content instead of raw HTML",
                    required=False,
                    default=True
                )
            ],
            permissions=[ToolPermission.READONLY],
            tags=["web", "fetch", "http"],
            examples=[
                {"arguments": {"url": "https://docs.python.org/3/tutorial/"}},
                {"arguments": {"url": "https://example.com", "extract_text": True}}
            ],
            returns={
                "type": "object",
                "properties": {
                    "content": {"type": "string"},
                    "title": {"type": "string"},
                    "status_code": {"type": "integer"}
                }
            }
        )
    ]


def register_all_tools() -> None:
    """Register all tool definitions with the global registry."""
    registry = get_registry()
    
    all_tools = (
        create_file_system_tools() +
        create_code_execution_tools() +
        create_git_tools() +
        create_testing_tools() +
        create_web_tools()
    )
    
    for tool_def in all_tools:
        # Register with placeholder handler - actual handlers in execution_engine.py
        registry.register(tool_def, lambda **kwargs: None, override=True)
    
    print(f"Registered {len(all_tools)} tools in the registry")


if __name__ == "__main__":
    register_all_tools()
    
    registry = get_registry()
    
    print("\n" + "="*60)
    print("REGISTERED TOOLS")
    print("="*60)
    
    for cat in ToolCategory:
        tools = registry.get_tool_by_category(cat)
        if tools:
            print(f"\n{cat.value.upper()}:")
            for tool in tools:
                print(f"  - {tool.name}: {tool.description[:60]}...")
    
    print("\n" + "="*60)
    print("REGISTRY STATS")
    print("="*60)
    import json
    print(json.dumps(registry.get_stats(), indent=2))
