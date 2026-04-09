"""
HelixLLM Tools - Main Integration Module
=========================================
Complete tool use system for 1.5B parameter LLMs.

This module integrates all components:
- Tool Registry
- Function Calling Pipeline
- Execution Engine
- Result Processing
- Fallback Strategies

Usage:
    from helixllm_tools import HelixLLMTools
    
    tools = HelixLLMTools()
    tools.initialize()
    
    result = tools.process_message("Read my main.py file")
"""

import os
import json
from typing import Dict, Any, List, Optional, Callable
from dataclasses import dataclass

# Import all components
from tool_registry import get_registry, ToolRegistry, ToolDefinition, ToolCategory, ToolPermission
from tool_definitions import register_all_tools
from function_caller import FunctionCaller, ResponseParser, PromptTemplate, ToolCall, CallStatus
from execution_engine import ExecutionEngine, ExecutionResult
from result_processor import ResultProcessor, OutputFormat, ErrorEnhancer
from fallback_strategies import FallbackStrategies, RetryManager, ConfirmationManager, AdaptiveCaller


@dataclass
class HelixConfig:
    """Configuration for HelixLLM Tools."""
    # Model settings
    max_tokens: int = 4096
    temperature: float = 0.1
    
    # Tool settings
    max_tools_in_prompt: int = 20
    enable_few_shot: bool = True
    
    # Execution settings
    default_timeout: int = 30
    max_retries: int = 3
    enable_security: bool = True
    allowed_paths: Optional[List[str]] = None
    
    # Result processing
    max_result_tokens: int = 2000
    enable_summarization: bool = True
    
    # Confirmation
    require_confirmation: bool = True
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "max_tokens": self.max_tokens,
            "temperature": self.temperature,
            "max_tools_in_prompt": self.max_tools_in_prompt,
            "enable_few_shot": self.enable_few_shot,
            "default_timeout": self.default_timeout,
            "max_retries": self.max_retries,
            "enable_security": self.enable_security,
            "max_result_tokens": self.max_result_tokens,
            "enable_summarization": self.enable_summarization,
            "require_confirmation": self.require_confirmation,
        }


class HelixLLMTools:
    """
    Main interface for HelixLLM tool system.
    
    Provides a unified API for:
    - Tool registration and discovery
    - Function calling with small models
    - Secure execution
    - Result processing
    - Error handling
    """
    
    def __init__(self, config: Optional[HelixConfig] = None):
        self.config = config or HelixConfig()
        
        # Initialize components
        self.registry = get_registry()
        self.execution_engine: Optional[ExecutionEngine] = None
        self.function_caller: Optional[FunctionCaller] = None
        self.result_processor = ResultProcessor()
        self.fallback_strategies = FallbackStrategies()
        self.error_enhancer = ErrorEnhancer()
        
        # State
        self.initialized = False
        self.conversation_history: List[Dict] = []
        
        # Model callback (to be set)
        self.model_callback: Optional[Callable[[List[Dict]], str]] = None
    
    def initialize(
        self,
        model_callback: Optional[Callable[[List[Dict]], str]] = None,
        working_dir: Optional[str] = None
    ) -> str:
        """
        Initialize the tool system.
        
        Args:
            model_callback: Function that takes messages and returns model response
            working_dir: Working directory for file operations
        
        Returns:
            System prompt for the model
        """
        # Register all tools
        register_all_tools()
        
        # Set up execution engine
        self.execution_engine = ExecutionEngine(
            working_dir=working_dir or os.getcwd(),
            enable_security=self.config.enable_security
        )
        
        # Configure allowed paths
        if self.config.allowed_paths:
            from execution_engine import SecurityValidator
            for path in self.config.allowed_paths:
                SecurityValidator.add_allowed_path(path)
        
        # Set up function caller
        self.function_caller = FunctionCaller(
            registry=self.registry,
            max_retries=self.config.max_retries,
            token_budget=self.config.max_tokens,
            confirmation_callback=self._confirmation_callback if self.config.require_confirmation else None
        )
        
        # Set model callback
        if model_callback:
            self.model_callback = model_callback
        
        # Register tool handlers
        self._register_handlers()
        
        # Initialize function caller
        system_prompt = self.function_caller.initialize(
            include_examples=self.config.enable_few_shot
        )
        
        self.initialized = True
        return system_prompt
    
    def _register_handlers(self) -> None:
        """Register all tool handlers with the registry."""
        handlers = {
            # File system
            "read_file": self._handle_read_file,
            "write_file": self._handle_write_file,
            "list_directory": self._handle_list_directory,
            "search_files": self._handle_search_files,
            "file_exists": self._handle_file_exists,
            
            # Code execution
            "execute_python": self._handle_execute_python,
            "execute_shell": self._handle_execute_shell,
            
            # Git
            "git_status": self._handle_git_status,
            "git_diff": self._handle_git_diff,
            "git_log": self._handle_git_log,
            "git_branch": self._handle_git_branch,
            
            # Testing & Analysis
            "run_tests": self._handle_run_tests,
            "analyze_code": self._handle_analyze_code,
            "get_dependencies": self._handle_get_dependencies,
            "calculate_complexity": self._handle_calculate_complexity,
            
            # Web
            "web_search": self._handle_web_search,
            "fetch_url": self._handle_fetch_url,
        }
        
        for tool_name, handler in handlers.items():
            tool_def = self.registry.get_tool(tool_name)
            if tool_def:
                self.registry.register(tool_def, handler, override=True)
    
    def _confirmation_callback(self, tool_name: str, arguments: Dict) -> bool:
        """Handle confirmation requests for destructive operations."""
        # In production, this would interact with the user
        # For now, log and allow (configurable)
        print(f"[CONFIRMATION] {tool_name}: {json.dumps(arguments, indent=2)}")
        return True  # Auto-confirm for testing
    
    # Tool Handlers
    
    def _handle_read_file(self, path: str, offset: int = 1, limit: int = 100) -> Dict:
        result = self.execution_engine.read_file(path, offset, limit)
        return result.to_dict()
    
    def _handle_write_file(self, path: str, content: str, append: bool = False) -> Dict:
        result = self.execution_engine.write_file(path, content, append)
        return result.to_dict()
    
    def _handle_list_directory(self, path: str, recursive: bool = False, show_hidden: bool = False) -> Dict:
        result = self.execution_engine.list_directory(path, recursive, show_hidden)
        return result.to_dict()
    
    def _handle_search_files(self, path: str, pattern: str, search_content: bool = False, file_pattern: str = "*") -> Dict:
        result = self.execution_engine.search_files(path, pattern, search_content, file_pattern)
        return result.to_dict()
    
    def _handle_file_exists(self, path: str) -> Dict:
        path = os.path.expanduser(path)
        exists = os.path.exists(path)
        file_type = "none"
        if exists:
            file_type = "directory" if os.path.isdir(path) else "file"
        return {"exists": exists, "type": file_type}
    
    def _handle_execute_python(self, code: str, timeout: int = 30, restart: bool = False) -> Dict:
        result = self.execution_engine.execute_python(code, timeout, restart)
        return result.to_dict()
    
    def _handle_execute_shell(self, command: str, timeout: int = 30, description: str = "") -> Dict:
        result = self.execution_engine.execute_shell(command, timeout, description)
        return result.to_dict()
    
    def _handle_git_status(self, path: str) -> Dict:
        result = self.execution_engine.execute_shell(
            f"cd {path} && git status --porcelain -b",
            description="Get git status"
        )
        
        if not result.success:
            return {"error": result.error_message}
        
        # Parse git status output
        lines = result.stdout.strip().split('\n')
        branch_line = lines[0] if lines else ""
        
        # Parse branch info
        branch = "unknown"
        ahead = 0
        behind = 0
        
        if branch_line.startswith("##"):
            branch_info = branch_line[3:].strip()
            if "..." in branch_info:
                branch = branch_info.split("...")[0]
                # Parse ahead/behind
                if "[ahead " in branch_info:
                    ahead_match = branch_info.split("[ahead ")[1].split("]")[0]
                    ahead = int(ahead_match.split(",")[0]) if "," in ahead_match else int(ahead_match)
                if "[behind " in branch_info:
                    behind_match = branch_info.split("[behind ")[1].split("]")[0]
                    behind = int(behind_match.split(",")[0]) if "," in behind_match else int(behind_match)
            else:
                branch = branch_info
        
        # Parse file status
        modified = []
        staged = []
        untracked = []
        
        for line in lines[1:]:
            if len(line) >= 2:
                index_status = line[0]
                worktree_status = line[1]
                filename = line[3:].strip()
                
                if index_status != ' ' and index_status != '?':
                    staged.append(filename)
                if worktree_status == 'M':
                    modified.append(filename)
                if index_status == '?':
                    untracked.append(filename)
        
        return {
            "branch": branch,
            "modified": modified,
            "staged": staged,
            "untracked": untracked,
            "ahead": ahead,
            "behind": behind
        }
    
    def _handle_git_diff(self, path: str, target: str = "", file: str = "") -> Dict:
        cmd = f"cd {path} && git diff"
        if target:
            cmd += f" {target}"
        if file:
            cmd += f" -- {file}"
        
        result = self.execution_engine.execute_shell(cmd, description="Get git diff")
        
        return {
            "diff": result.stdout,
            "files_changed": []  # Could parse from diff
        }
    
    def _handle_git_log(self, path: str, limit: int = 10, file: str = "", author: str = "") -> Dict:
        cmd = f"cd {path} && git log --pretty=format:'%H|%an|%ad|%s' --date=short -{limit}"
        
        if file:
            cmd += f" -- {file}"
        if author:
            cmd += f" --author='{author}'"
        
        result = self.execution_engine.execute_shell(cmd, description="Get git log")
        
        commits = []
        for line in result.stdout.strip().split('\n'):
            if '|' in line:
                parts = line.split('|', 3)
                if len(parts) >= 4:
                    commits.append({
                        "hash": parts[0][:8],
                        "author": parts[1],
                        "date": parts[2],
                        "message": parts[3]
                    })
        
        return {"commits": commits}
    
    def _handle_git_branch(self, path: str, all: bool = False) -> Dict:
        cmd = f"cd {path} && git branch"
        if all:
            cmd += " -a"
        
        result = self.execution_engine.execute_shell(cmd, description="Get git branches")
        
        branches = []
        current = ""
        
        for line in result.stdout.strip().split('\n'):
            line = line.strip()
            if line.startswith('*'):
                current = line[2:].strip()
                branches.append(current)
            elif line:
                branches.append(line)
        
        return {"current": current, "branches": branches}
    
    def _handle_run_tests(self, path: str, test_path: str = "", framework: str = "auto", verbose: bool = True) -> Dict:
        # Auto-detect framework
        if framework == "auto":
            if os.path.exists(os.path.join(path, "pytest.ini")) or os.path.exists(os.path.join(path, "pyproject.toml")):
                framework = "pytest"
            elif os.path.exists(os.path.join(path, "package.json")):
                framework = "jest"
            else:
                framework = "pytest"  # Default
        
        if framework == "pytest":
            cmd = f"cd {path} && python -m pytest"
            if test_path:
                cmd += f" {test_path}"
            if verbose:
                cmd += " -v"
            cmd += " --tb=short"
        else:
            return {"error": f"Framework {framework} not yet supported"}
        
        result = self.execution_engine.execute_shell(cmd, timeout=300, description="Run tests")
        
        # Parse pytest output
        passed = 0
        failed = 0
        skipped = 0
        
        # Look for summary line
        for line in result.stdout.split('\n'):
            if 'passed' in line or 'failed' in line or 'error' in line:
                # Parse counts
                parts = line.split(',')
                for part in parts:
                    if 'passed' in part:
                        passed = int(part.strip().split()[0])
                    elif 'failed' in part:
                        failed = int(part.strip().split()[0])
                    elif 'skipped' in part:
                        skipped = int(part.strip().split()[0])
        
        return {
            "passed": passed,
            "failed": failed,
            "skipped": skipped,
            "duration": result.execution_time,
            "failures": []  # Could parse from output
        }
    
    def _handle_analyze_code(self, path: str, language: str = "auto", include_metrics: bool = True) -> Dict:
        # This is a simplified version - in production would use proper linters
        issues = []
        metrics = {}
        
        if os.path.isfile(path):
            # Analyze single file
            with open(path, 'r') as f:
                content = f.read()
            
            lines = content.split('\n')
            metrics = {
                "total_lines": len(lines),
                "code_lines": len([l for l in lines if l.strip() and not l.strip().startswith('#')]),
                "comment_lines": len([l for l in lines if l.strip().startswith('#')]),
            }
            
            # Simple checks
            for i, line in enumerate(lines, 1):
                if len(line) > 120:
                    issues.append({
                        "severity": "warning",
                        "message": "Line too long",
                        "line": i,
                        "file": path
                    })
        
        return {
            "issues": issues,
            "metrics": metrics
        }
    
    def _handle_get_dependencies(self, path: str, format: str = "list") -> Dict:
        dependencies = []
        dev_dependencies = []
        
        # Check for Python requirements
        req_file = os.path.join(path, "requirements.txt")
        if os.path.exists(req_file):
            with open(req_file, 'r') as f:
                for line in f:
                    line = line.strip()
                    if line and not line.startswith('#'):
                        dependencies.append(line)
        
        # Check for package.json
        pkg_file = os.path.join(path, "package.json")
        if os.path.exists(pkg_file):
            with open(pkg_file, 'r') as f:
                import json
                pkg = json.load(f)
                dependencies.extend([f"{k}@{v}" for k, v in pkg.get("dependencies", {}).items()])
                dev_dependencies.extend([f"{k}@{v}" for k, v in pkg.get("devDependencies", {}).items()])
        
        return {
            "dependencies": dependencies,
            "dev_dependencies": dev_dependencies,
            "outdated": []
        }
    
    def _handle_calculate_complexity(self, path: str, threshold: int = 10) -> Dict:
        # Simplified complexity calculation
        functions = []
        
        if os.path.isfile(path) and path.endswith('.py'):
            with open(path, 'r') as f:
                content = f.read()
            
            import re
            # Find function definitions
            for match in re.finditer(r'def\s+(\w+)\s*\(', content):
                func_name = match.group(1)
                # Simple complexity estimate based on keywords
                func_start = match.start()
                next_def = content.find('\ndef ', func_start + 1)
                func_body = content[func_start:next_def if next_def > 0 else len(content)]
                
                complexity = 1
                complexity += func_body.count('if ')
                complexity += func_body.count('for ')
                complexity += func_body.count('while ')
                complexity += func_body.count('except:')
                
                functions.append({
                    "name": func_name,
                    "complexity": complexity,
                    "line": content[:func_start].count('\n') + 1
                })
        
        avg_complexity = sum(f["complexity"] for f in functions) / len(functions) if functions else 0
        max_complexity = max((f["complexity"] for f in functions), default=0)
        
        return {
            "average_complexity": round(avg_complexity, 2),
            "max_complexity": max_complexity,
            "functions": functions
        }
    
    def _handle_web_search(self, queries: List[str], total_count: int = 10) -> Dict:
        # Placeholder - would integrate with actual search API
        return {
            "results": [
                {"title": f"Result for: {q}", "snippet": "Search integration needed", "url": ""}
                for q in queries[:total_count]
            ]
        }
    
    def _handle_fetch_url(self, url: str, extract_text: bool = True) -> Dict:
        # Placeholder - would integrate with actual fetch
        return {
            "content": f"Fetch from {url} - integration needed",
            "title": "",
            "status_code": 200
        }
    
    # Public API
    
    def process_message(
        self,
        user_message: str,
        model_callback: Optional[Callable[[List[Dict]], str]] = None
    ) -> Dict[str, Any]:
        """
        Process a user message and handle any tool calls.
        
        Args:
            user_message: The user's input message
            model_callback: Optional override for model callback
        
        Returns:
            Dictionary with final_response, tool_calls, and conversation
        """
        if not self.initialized:
            raise RuntimeError("HelixLLMTools not initialized. Call initialize() first.")
        
        callback = model_callback or self.model_callback
        if not callback:
            raise ValueError("No model callback provided")
        
        return self.function_caller.process_user_message(user_message, callback)
    
    def get_system_prompt(self) -> str:
        """Get the system prompt for the model."""
        if not self.initialized:
            raise RuntimeError("HelixLLMTools not initialized")
        return self.function_caller.system_prompt
    
    def get_available_tools(self) -> List[str]:
        """Get list of available tool names."""
        return self.registry.list_tools()
    
    def get_tool_info(self, tool_name: str) -> Optional[Dict]:
        """Get information about a specific tool."""
        tool = self.registry.get_tool(tool_name)
        if tool:
            return {
                "name": tool.name,
                "description": tool.description,
                "category": tool.category.value,
                "parameters": [
                    {
                        "name": p.name,
                        "type": p.type,
                        "required": p.required,
                        "description": p.description
                    }
                    for p in tool.parameters
                ]
            }
        return None
    
    def clear_history(self) -> None:
        """Clear conversation history."""
        if self.function_caller:
            self.function_caller.clear_history()
    
    def get_stats(self) -> Dict[str, Any]:
        """Get tool usage statistics."""
        if self.function_caller:
            return self.function_caller.get_stats()
        return {}


# Convenience function for quick setup
def create_tools(
    model_callback: Callable[[List[Dict]], str],
    working_dir: Optional[str] = None,
    config: Optional[HelixConfig] = None
) -> HelixLLMTools:
    """
    Create and initialize HelixLLMTools with common settings.
    
    Args:
        model_callback: Function that takes messages and returns model response
        working_dir: Working directory for file operations
        config: Optional custom configuration
    
    Returns:
        Initialized HelixLLMTools instance
    """
    tools = HelixLLMTools(config)
    tools.initialize(model_callback=model_callback, working_dir=working_dir)
    return tools


if __name__ == "__main__":
    # Demo
    print("="*60)
    print("HELIXLLM TOOLS - MAIN MODULE")
    print("="*60)
    
    # Mock model callback for testing
    def mock_model(messages: List[Dict]) -> str:
        # Simulate a tool call response
        return '''<function_calls>
<invoke name="list_directory">
<parameter name="path">/mnt/okcomputer</parameter>
</invoke>
</function_calls>'''
    
    # Create and initialize tools
    tools = create_tools(mock_model, working_dir="/mnt/okcomputer")
    
    print("\nSystem Prompt (truncated):")
    print(tools.get_system_prompt()[:1000])
    print("\n... [truncated] ...")
    
    print("\nAvailable Tools:")
    for tool_name in tools.get_available_tools()[:10]:
        print(f"  - {tool_name}")
    
    print("\nTool Info Example:")
    info = tools.get_tool_info("read_file")
    if info:
        print(f"  Name: {info['name']}")
        print(f"  Description: {info['description'][:60]}...")
        print(f"  Parameters: {[p['name'] for p in info['parameters']]}")
    
    # Test processing
    print("\nProcessing message...")
    result = tools.process_message("List files in the current directory")
    print(f"Final Response: {result['final_response'][:200]}...")
    print(f"Tool Calls: {len(result['tool_calls'])}")
