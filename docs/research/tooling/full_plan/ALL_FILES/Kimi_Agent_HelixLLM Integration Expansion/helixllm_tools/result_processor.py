"""
HelixLLM Tool Result Processing
================================
Process and format tool results for LLM consumption.

Features:
- Result truncation for large outputs
- Format conversion
- Error message enhancement
- Token budget management
- Smart summarization
"""

import json
import re
from typing import Dict, Any, List, Optional, Callable, Union
from dataclasses import dataclass
from enum import Enum


class OutputFormat(str, Enum):
    """Output formats for tool results."""
    RAW = "raw"
    JSON = "json"
    MARKDOWN = "markdown"
    SUMMARY = "summary"
    TRUNCATED = "truncated"


@dataclass
class ProcessingConfig:
    """Configuration for result processing."""
    max_tokens: int = 2000
    max_lines: int = 100
    max_list_items: int = 50
    max_string_length: int = 500
    enable_summarization: bool = True
    preserve_structure: bool = True
    include_metadata: bool = True


class ResultProcessor:
    """
    Process tool results for optimal LLM consumption.
    
    Key features:
    - Token-aware truncation
    - Structure preservation
    - Smart summarization
    - Format conversion
    """
    
    # Approximate tokens per character (rough estimate)
    TOKENS_PER_CHAR = 0.25
    
    def __init__(self, config: Optional[ProcessingConfig] = None):
        self.config = config or ProcessingConfig()
        self._summarizers: Dict[str, Callable] = {
            "file_content": self._summarize_file_content,
            "directory_listing": self._summarize_directory,
            "search_results": self._summarize_search,
            "code_analysis": self._summarize_code_analysis,
            "test_results": self._summarize_test_results,
            "git_status": self._summarize_git_status,
            "python_output": self._summarize_python_output,
            "shell_output": self._summarize_shell_output,
        }
    
    def process(
        self,
        result: Any,
        result_type: str = "generic",
        format: OutputFormat = OutputFormat.TRUNCATED
    ) -> Dict[str, Any]:
        """
        Process a tool result for LLM consumption.
        
        Args:
            result: The raw tool result
            result_type: Type of result for specialized processing
            format: Desired output format
        
        Returns:
            Processed result dictionary
        """
        if result is None:
            return {"content": "No result", "truncated": False}
        
        # Handle ExecutionResult objects
        if hasattr(result, 'to_dict'):
            result = result.to_dict()
        
        # Apply format-specific processing
        if format == OutputFormat.RAW:
            return self._format_raw(result)
        elif format == OutputFormat.JSON:
            return self._format_json(result)
        elif format == OutputFormat.MARKDOWN:
            return self._format_markdown(result, result_type)
        elif format == OutputFormat.SUMMARY:
            return self._format_summary(result, result_type)
        else:  # TRUNCATED
            return self._format_truncated(result, result_type)
    
    def _format_raw(self, result: Any) -> Dict[str, Any]:
        """Return result in raw format."""
        return {
            "content": result,
            "truncated": False,
            "format": "raw"
        }
    
    def _format_json(self, result: Any) -> Dict[str, Any]:
        """Format result as JSON string."""
        try:
            json_str = json.dumps(result, indent=2, default=str)
            truncated, json_str = self._truncate_string(json_str)
            return {
                "content": json_str,
                "truncated": truncated,
                "format": "json"
            }
        except:
            return self._format_raw(result)
    
    def _format_markdown(self, result: Any, result_type: str) -> Dict[str, Any]:
        """Format result as Markdown."""
        markdown = self._convert_to_markdown(result, result_type)
        truncated, markdown = self._truncate_string(markdown)
        return {
            "content": markdown,
            "truncated": truncated,
            "format": "markdown"
        }
    
    def _format_summary(self, result: Any, result_type: str) -> Dict[str, Any]:
        """Format result as a summary."""
        summarizer = self._summarizers.get(result_type, self._generic_summarizer)
        summary = summarizer(result)
        return {
            "content": summary,
            "truncated": False,
            "format": "summary"
        }
    
    def _format_truncated(self, result: Any, result_type: str) -> Dict[str, Any]:
        """Format with intelligent truncation."""
        # First try to get a summary if enabled
        if self.config.enable_summarization:
            summarizer = self._summarizers.get(result_type)
            if summarizer:
                summary = summarizer(result)
                if self._estimate_tokens(summary) <= self.config.max_tokens:
                    return {
                        "content": summary,
                        "truncated": False,
                        "format": "summary"
                    }
        
        # Fall back to structured truncation
        truncated_result = self._truncate_structure(result)
        
        return {
            "content": truncated_result,
            "truncated": True,
            "format": "truncated"
        }
    
    def _truncate_structure(self, data: Any, depth: int = 0) -> Any:
        """
        Recursively truncate data structure while preserving shape.
        """
        if depth > 5:  # Limit nesting depth
            return "..."
        
        if isinstance(data, dict):
            truncated = {}
            for i, (key, value) in enumerate(data.items()):
                if i >= self.config.max_list_items:
                    truncated["..."] = f"({len(data) - i} more items)"
                    break
                truncated[key] = self._truncate_structure(value, depth + 1)
            return truncated
        
        elif isinstance(data, list):
            truncated = []
            for i, item in enumerate(data):
                if i >= self.config.max_list_items:
                    truncated.append(f"... ({len(data) - i} more items)")
                    break
                truncated.append(self._truncate_structure(item, depth + 1))
            return truncated
        
        elif isinstance(data, str):
            if len(data) > self.config.max_string_length:
                return data[:self.config.max_string_length] + "... [truncated]"
            return data
        
        else:
            return data
    
    def _truncate_string(self, text: str) -> tuple:
        """
        Truncate a string to fit within token budget.
        
        Returns:
            (was_truncated, truncated_text)
        """
        max_chars = int(self.config.max_tokens / self.TOKENS_PER_CHAR)
        
        if len(text) <= max_chars:
            return False, text
        
        # Truncate at line boundary if possible
        truncated = text[:max_chars]
        last_newline = truncated.rfind('\n')
        
        if last_newline > max_chars * 0.8:
            truncated = truncated[:last_newline]
        
        return True, truncated + "\n\n... [output truncated due to length]"
    
    def _estimate_tokens(self, text: str) -> int:
        """Estimate token count for text."""
        return int(len(text) * self.TOKENS_PER_CHAR)
    
    def _convert_to_markdown(self, result: Any, result_type: str) -> str:
        """Convert result to Markdown format."""
        if isinstance(result, dict):
            if "content" in result and isinstance(result["content"], str):
                return f"```\n{result['content']}\n```"
            
            md_parts = []
            for key, value in result.items():
                md_parts.append(f"**{key}:** {self._value_to_markdown(value)}")
            return "\n".join(md_parts)
        
        elif isinstance(result, list):
            return "\n".join([f"- {self._value_to_markdown(item)}" for item in result[:self.config.max_list_items]])
        
        else:
            return str(result)
    
    def _value_to_markdown(self, value: Any) -> str:
        """Convert a single value to Markdown."""
        if isinstance(value, dict):
            return json.dumps(value, indent=2)
        elif isinstance(value, list):
            if len(value) > 5:
                return f"[{', '.join(str(v) for v in value[:5])}... ({len(value)} items)]"
            return str(value)
        else:
            return str(value)
    
    # Specialized summarizers
    
    def _summarize_file_content(self, result: Any) -> str:
        """Summarize file content result."""
        if isinstance(result, dict):
            content = result.get("data", {}).get("content", "")
            lines_read = result.get("data", {}).get("lines_read", 0)
            total_lines = result.get("data", {}).get("total_lines", 0)
            truncated = result.get("data", {}).get("truncated", False)
            
            summary = f"File content ({lines_read}/{total_lines} lines shown):\n```\n{content}\n```"
            if truncated:
                summary += "\n[File was truncated]"
            return summary
        return str(result)
    
    def _summarize_directory(self, result: Any) -> str:
        """Summarize directory listing."""
        if isinstance(result, dict):
            data = result.get("data", {})
            path = data.get("path", "")
            entries = data.get("entries", [])
            
            files = [e for e in entries if e.get("type") == "file"]
            dirs = [e for e in entries if e.get("type") == "directory"]
            
            summary = f"Directory: {path}\n"
            summary += f"Files: {len(files)}, Directories: {len(dirs)}\n\n"
            
            if dirs:
                summary += "Directories:\n"
                for d in dirs[:20]:
                    summary += f"  📁 {d['name']}\n"
                if len(dirs) > 20:
                    summary += f"  ... and {len(dirs) - 20} more\n"
            
            if files:
                summary += "\nFiles:\n"
                for f in files[:30]:
                    size = self._format_size(f.get("size", 0))
                    summary += f"  📄 {f['name']} ({size})\n"
                if len(files) > 30:
                    summary += f"  ... and {len(files) - 30} more\n"
            
            return summary
        return str(result)
    
    def _summarize_search(self, result: Any) -> str:
        """Summarize search results."""
        if isinstance(result, dict):
            data = result.get("data", {})
            matches = data.get("matches", [])
            total = data.get("total_matches", len(matches))
            
            summary = f"Search Results: {total} matches found\n\n"
            
            for i, match in enumerate(matches[:20]):
                path = match.get("path", "")
                line = match.get("line", 0)
                content = match.get("content", "")[:100]
                summary += f"{i+1}. {path}:{line}\n   {content}\n\n"
            
            if len(matches) > 20:
                summary += f"... and {len(matches) - 20} more matches\n"
            
            return summary
        return str(result)
    
    def _summarize_code_analysis(self, result: Any) -> str:
        """Summarize code analysis results."""
        if isinstance(result, dict):
            data = result.get("data", {})
            issues = data.get("issues", [])
            metrics = data.get("metrics", {})
            
            summary = "Code Analysis Results:\n\n"
            
            if metrics:
                summary += "Metrics:\n"
                for key, value in metrics.items():
                    summary += f"  {key}: {value}\n"
                summary += "\n"
            
            if issues:
                errors = [i for i in issues if i.get("severity") == "error"]
                warnings = [i for i in issues if i.get("severity") == "warning"]
                
                summary += f"Issues: {len(errors)} errors, {len(warnings)} warnings\n\n"
                
                for issue in issues[:15]:
                    severity = issue.get("severity", "info")
                    msg = issue.get("message", "")
                    file = issue.get("file", "")
                    line = issue.get("line", 0)
                    summary += f"[{severity.upper()}] {file}:{line} - {msg}\n"
                
                if len(issues) > 15:
                    summary += f"... and {len(issues) - 15} more issues\n"
            else:
                summary += "No issues found!\n"
            
            return summary
        return str(result)
    
    def _summarize_test_results(self, result: Any) -> str:
        """Summarize test results."""
        if isinstance(result, dict):
            data = result.get("data", {})
            passed = data.get("passed", 0)
            failed = data.get("failed", 0)
            skipped = data.get("skipped", 0)
            duration = data.get("duration", 0)
            failures = data.get("failures", [])
            
            summary = "Test Results:\n"
            summary += f"  Passed: {passed}\n"
            summary += f"  Failed: {failed}\n"
            summary += f"  Skipped: {skipped}\n"
            summary += f"  Duration: {duration:.2f}s\n\n"
            
            if failed > 0 and failures:
                summary += "Failures:\n"
                for failure in failures[:10]:
                    summary += f"  - {failure}\n"
            
            return summary
        return str(result)
    
    def _summarize_git_status(self, result: Any) -> str:
        """Summarize git status."""
        if isinstance(result, dict):
            data = result.get("data", {})
            branch = data.get("branch", "")
            modified = data.get("modified", [])
            staged = data.get("staged", [])
            untracked = data.get("untracked", [])
            
            summary = f"Git Status (branch: {branch}):\n\n"
            
            if staged:
                summary += f"Staged ({len(staged)}):\n"
                for f in staged[:10]:
                    summary += f"  + {f}\n"
                if len(staged) > 10:
                    summary += f"  ... and {len(staged) - 10} more\n"
                summary += "\n"
            
            if modified:
                summary += f"Modified ({len(modified)}):\n"
                for f in modified[:10]:
                    summary += f"  M {f}\n"
                if len(modified) > 10:
                    summary += f"  ... and {len(modified) - 10} more\n"
                summary += "\n"
            
            if untracked:
                summary += f"Untracked ({len(untracked)}):\n"
                for f in untracked[:10]:
                    summary += f"  ? {f}\n"
                if len(untracked) > 10:
                    summary += f"  ... and {len(untracked) - 10} more\n"
            
            if not any([staged, modified, untracked]):
                summary += "Working tree clean\n"
            
            return summary
        return str(result)
    
    def _summarize_python_output(self, result: Any) -> str:
        """Summarize Python execution output."""
        if isinstance(result, dict):
            stdout = result.get("stdout", "")
            stderr = result.get("stderr", "")
            success = result.get("success", False)
            
            summary = "Python Execution:\n"
            summary += f"Success: {success}\n\n"
            
            if stdout:
                summary += f"Output:\n```\n{stdout[:1000]}\n```\n"
            
            if stderr:
                summary += f"\nErrors:\n```\n{stderr[:500]}\n```\n"
            
            return summary
        return str(result)
    
    def _summarize_shell_output(self, result: Any) -> str:
        """Summarize shell command output."""
        if isinstance(result, dict):
            stdout = result.get("stdout", "")
            stderr = result.get("stderr", "")
            exit_code = result.get("data", {}).get("exit_code", -1)
            
            summary = f"Shell Output (exit code: {exit_code}):\n\n"
            
            if stdout:
                summary += f"```\n{stdout[:1000]}\n```\n"
            
            if stderr:
                summary += f"\nStderr:\n```\n{stderr[:500]}\n```\n"
            
            return summary
        return str(result)
    
    def _generic_summarizer(self, result: Any) -> str:
        """Generic summarizer for unknown result types."""
        if isinstance(result, dict):
            return json.dumps(result, indent=2, default=str)[:self.config.max_tokens * 4]
        elif isinstance(result, list):
            return json.dumps(result[:self.config.max_list_items], indent=2)
        else:
            return str(result)[:self.config.max_tokens * 4]
    
    def _format_size(self, size_bytes: int) -> str:
        """Format byte size to human readable."""
        for unit in ['B', 'KB', 'MB', 'GB']:
            if size_bytes < 1024:
                return f"{size_bytes:.1f} {unit}"
            size_bytes /= 1024
        return f"{size_bytes:.1f} TB"
    
    def register_summarizer(self, result_type: str, summarizer: Callable) -> None:
        """Register a custom summarizer."""
        self._summarizers[result_type] = summarizer
    
    def get_token_estimate(self, result: Any) -> int:
        """Estimate token count for a result."""
        text = json.dumps(result, default=str)
        return self._estimate_tokens(text)


class ErrorEnhancer:
    """
    Enhance error messages for better LLM understanding.
    """
    
    ERROR_TEMPLATES = {
        "file_not_found": "File not found: {path}. Please check the path and try again.",
        "permission_denied": "Permission denied: {path}. You may not have access to this location.",
        "command_not_found": "Command not found: {command}. Is it installed and in your PATH?",
        "timeout": "Operation timed out after {timeout}s. The operation may be too complex.",
        "invalid_argument": "Invalid argument: {argument}. Expected: {expected}",
        "missing_required": "Missing required parameter: {parameter}",
        "type_error": "Type error: expected {expected}, got {actual}",
    }
    
    def enhance(self, error: str, context: Dict[str, Any] = None) -> str:
        """
        Enhance an error message with helpful context.
        """
        context = context or {}
        
        # Try to match error patterns
        for error_type, template in self.ERROR_TEMPLATES.items():
            if error_type.replace("_", " ") in error.lower():
                try:
                    return template.format(**context)
                except:
                    pass
        
        # Add general guidance
        enhanced = f"Error: {error}\n\n"
        enhanced += "Suggestions:\n"
        
        if "not found" in error.lower():
            enhanced += "- Check that the path or name is correct\n"
            enhanced += "- Verify the resource exists\n"
        
        if "permission" in error.lower():
            enhanced += "- Check your access permissions\n"
            enhanced += "- Try a different location\n"
        
        if "timeout" in error.lower():
            enhanced += "- Try a more specific query\n"
            enhanced += "- Break the operation into smaller steps\n"
        
        if "invalid" in error.lower():
            enhanced += "- Check the parameter format\n"
            enhanced += "- Review the tool documentation\n"
        
        return enhanced


if __name__ == "__main__":
    # Demo
    processor = ResultProcessor()
    
    print("="*60)
    print("RESULT PROCESSOR DEMO")
    print("="*60)
    
    # Test file content result
    file_result = {
        "success": True,
        "data": {
            "content": "line 1\nline 2\nline 3",
            "lines_read": 3,
            "total_lines": 100,
            "truncated": True
        }
    }
    
    print("\n1. File Content Summary:")
    processed = processor.process(file_result, "file_content", OutputFormat.SUMMARY)
    print(processed["content"])
    
    # Test directory listing
    dir_result = {
        "success": True,
        "data": {
            "path": "/home/user/project",
            "entries": [
                {"name": "src", "type": "directory"},
                {"name": "tests", "type": "directory"},
                {"name": "main.py", "type": "file", "size": 1024},
                {"name": "README.md", "type": "file", "size": 512},
            ]
        }
    }
    
    print("\n2. Directory Listing Summary:")
    processed = processor.process(dir_result, "directory_listing", OutputFormat.SUMMARY)
    print(processed["content"])
    
    # Test search results
    search_result = {
        "success": True,
        "data": {
            "matches": [
                {"path": "/a.py", "line": 10, "content": "def process_data():"},
                {"path": "/b.py", "line": 25, "content": "def process_data(x):"},
            ],
            "total_matches": 2
        }
    }
    
    print("\n3. Search Results Summary:")
    processed = processor.process(search_result, "search_results", OutputFormat.SUMMARY)
    print(processed["content"])
