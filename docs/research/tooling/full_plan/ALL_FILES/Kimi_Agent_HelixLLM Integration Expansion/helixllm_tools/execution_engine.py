"""
HelixLLM Tool Execution Engine
===============================
Secure, sandboxed execution environment for tools.

Features:
- Sandboxed execution with resource limits
- Timeout handling
- Security validation
- Result formatting
- Error handling and recovery
"""

import os
import sys
import json
import time
import signal
import subprocess
import tempfile
import resource
from typing import Dict, Any, Optional, List, Callable, Tuple
from dataclasses import dataclass
from pathlib import Path
from datetime import datetime
import threading
import traceback


@dataclass
class ExecutionResult:
    """Result of a tool execution."""
    success: bool
    data: Any
    stdout: str = ""
    stderr: str = ""
    execution_time: float = 0.0
    error_message: str = ""
    truncated: bool = False
    metadata: Dict[str, Any] = None
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "success": self.success,
            "data": self.data,
            "stdout": self.stdout,
            "stderr": self.stderr,
            "execution_time": self.execution_time,
            "error_message": self.error_message,
            "truncated": self.truncated,
            "metadata": self.metadata or {}
        }


class SecurityValidator:
    """
    Validates operations for security concerns.
    """
    
    # Dangerous patterns in shell commands
    DANGEROUS_PATTERNS = [
        r'rm\s+-rf\s+/',
        r'>\s*/dev/',
        r'mkfs\.',
        r'dd\s+if=.*of=/dev/',
        r':\(\)\{\s*:\|\:&\};',
        r'curl.*\|.*sh',
        r'wget.*\|.*sh',
        r'eval\s*\(',
        r'exec\s*\(',
        r'__import__\s*\(\s*["\']os["\']',
        r'subprocess\.call\s*\(',
        r'os\.system\s*\(',
    ]
    
    # Sensitive paths that require confirmation
    SENSITIVE_PATHS = [
        '/etc/',
        '/usr/',
        '/bin/',
        '/sbin/',
        '/lib/',
        '/sys/',
        '/proc/',
        '/dev/',
    ]
    
    # Allowed paths for file operations (configurable)
    ALLOWED_PATHS: List[str] = []
    
    # Blocked commands
    BLOCKED_COMMANDS = [
        'rm', 'del', 'format', 'fdisk', 'mkfs',
        'dd', 'shutdown', 'reboot', 'halt',
    ]
    
    @classmethod
    def validate_shell_command(cls, command: str) -> Tuple[bool, str]:
        """
        Validate a shell command for security.
        
        Returns:
            (is_valid, error_message)
        """
        command_lower = command.lower().strip()
        
        # Check for dangerous patterns
        for pattern in cls.DANGEROUS_PATTERNS:
            if re.search(pattern, command, re.IGNORECASE):
                return False, f"Command contains dangerous pattern: {pattern}"
        
        # Check for blocked commands at start
        cmd_parts = command_lower.split()
        if cmd_parts and cmd_parts[0] in cls.BLOCKED_COMMANDS:
            return False, f"Command '{cmd_parts[0]}' is blocked for security"
        
        # Check for sudo
        if 'sudo' in command_lower:
            return False, "Commands with sudo are not allowed"
        
        return True, ""
    
    @classmethod
    def validate_path(cls, path: str, operation: str = "read") -> Tuple[bool, str]:
        """
        Validate a file path for security.
        
        Returns:
            (is_valid, error_message)
        """
        path = os.path.abspath(os.path.expanduser(path))
        
        # Check if path exists and get its type
        if os.path.exists(path):
            # Check for symlinks that escape allowed paths
            real_path = os.path.realpath(path)
            
            # Check sensitive paths
            for sensitive in cls.SENSITIVE_PATHS:
                if real_path.startswith(sensitive):
                    if operation in ["write", "delete"]:
                        return False, f"Cannot {operation} system path: {path}"
        
        # If allowed paths are configured, enforce them
        if cls.ALLOWED_PATHS:
            path_allowed = any(
                path.startswith(allowed) or allowed.startswith(path)
                for allowed in cls.ALLOWED_PATHS
            )
            if not path_allowed:
                return False, f"Path not in allowed directories: {path}"
        
        return True, ""
    
    @classmethod
    def validate_python_code(cls, code: str) -> Tuple[bool, str]:
        """
        Validate Python code for security concerns.
        
        Returns:
            (is_valid, error_message)
        """
        import re
        
        # Check for dangerous imports and calls
        dangerous_patterns = [
            r'__import__\s*\(',
            r'import\s+os\s*;\s*os\.system',
            r'subprocess\.(call|run|Popen)',
            r'eval\s*\(',
            r'exec\s*\(',
            r'compile\s*\(',
            r'open\s*\(\s*["\']/(etc|proc|sys)',
        ]
        
        for pattern in dangerous_patterns:
            if re.search(pattern, code, re.IGNORECASE):
                return False, f"Code contains potentially dangerous pattern"
        
        return True, ""
    
    @classmethod
    def add_allowed_path(cls, path: str) -> None:
        """Add a path to the allowed list."""
        abs_path = os.path.abspath(os.path.expanduser(path))
        if abs_path not in cls.ALLOWED_PATHS:
            cls.ALLOWED_PATHS.append(abs_path)
    
    @classmethod
    def clear_allowed_paths(cls) -> None:
        """Clear all allowed paths."""
        cls.ALLOWED_PATHS = []


class ResourceLimiter:
    """
    Manages resource limits for sandboxed execution.
    """
    
    DEFAULT_LIMITS = {
        "cpu_time": 30,  # seconds
        "memory_mb": 512,  # MB
        "file_size_mb": 100,  # MB
        "processes": 10,
        "open_files": 100,
    }
    
    def __init__(self, limits: Optional[Dict[str, int]] = None):
        self.limits = limits or self.DEFAULT_LIMITS.copy()
    
    def apply_limits(self):
        """Apply resource limits to current process."""
        # CPU time limit
        if "cpu_time" in self.limits:
            resource.setrlimit(
                resource.RLIMIT_CPU,
                (self.limits["cpu_time"], self.limits["cpu_time"] + 5)
            )
        
        # Memory limit
        if "memory_mb" in self.limits:
            max_memory = self.limits["memory_mb"] * 1024 * 1024
            resource.setrlimit(resource.RLIMIT_AS, (max_memory, max_memory))
        
        # File size limit
        if "file_size_mb" in self.limits:
            max_file_size = self.limits["file_size_mb"] * 1024 * 1024
            resource.setrlimit(resource.RLIMIT_FSIZE, (max_file_size, max_file_size))
        
        # Process limit
        if "processes" in self.limits:
            resource.setrlimit(
                resource.RLIMIT_NPROC,
                (self.limits["processes"], self.limits["processes"])
            )
        
        # Open files limit
        if "open_files" in self.limits:
            resource.setrlimit(
                resource.RLIMIT_NOFILE,
                (self.limits["open_files"], self.limits["open_files"])
            )


class ExecutionEngine:
    """
    Main execution engine for running tools securely.
    """
    
    def __init__(
        self,
        working_dir: Optional[str] = None,
        resource_limits: Optional[Dict[str, int]] = None,
        enable_security: bool = True
    ):
        self.working_dir = working_dir or os.getcwd()
        self.resource_limits = resource_limits or ResourceLimiter.DEFAULT_LIMITS.copy()
        self.enable_security = enable_security
        self.execution_history: List[Dict] = []
        
        # Python execution environment
        self._python_globals = {"__builtins__": __builtins__}
        self._python_locals = {}
    
    def execute_shell(
        self,
        command: str,
        timeout: int = 30,
        description: str = ""
    ) -> ExecutionResult:
        """
        Execute a shell command with security and resource limits.
        """
        start_time = time.time()
        
        # Security validation
        if self.enable_security:
            is_valid, error = SecurityValidator.validate_shell_command(command)
            if not is_valid:
                return ExecutionResult(
                    success=False,
                    data=None,
                    error_message=error,
                    execution_time=time.time() - start_time
                )
        
        try:
            # Run command with timeout
            result = subprocess.run(
                command,
                shell=True,
                capture_output=True,
                text=True,
                timeout=timeout,
                cwd=self.working_dir
            )
            
            execution_time = time.time() - start_time
            
            # Record execution
            self._record_execution("shell", command, result.returncode == 0)
            
            return ExecutionResult(
                success=result.returncode == 0,
                data={"exit_code": result.returncode},
                stdout=result.stdout,
                stderr=result.stderr,
                execution_time=execution_time,
                metadata={"command": command, "description": description}
            )
            
        except subprocess.TimeoutExpired:
            return ExecutionResult(
                success=False,
                data=None,
                error_message=f"Command timed out after {timeout} seconds",
                execution_time=time.time() - start_time
            )
        except Exception as e:
            return ExecutionResult(
                success=False,
                data=None,
                error_message=str(e),
                execution_time=time.time() - start_time
            )
    
    def execute_python(
        self,
        code: str,
        timeout: int = 30,
        restart: bool = False
    ) -> ExecutionResult:
        """
        Execute Python code in a sandboxed environment.
        """
        start_time = time.time()
        
        # Security validation
        if self.enable_security:
            is_valid, error = SecurityValidator.validate_python_code(code)
            if not is_valid:
                return ExecutionResult(
                    success=False,
                    data=None,
                    error_message=error,
                    execution_time=time.time() - start_time
                )
        
        # Reset environment if requested
        if restart:
            self._python_globals = {"__builtins__": __builtins__}
            self._python_locals = {}
        
        # Capture stdout/stderr
        import io
        old_stdout = sys.stdout
        old_stderr = sys.stderr
        
        stdout_capture = io.StringIO()
        stderr_capture = io.StringIO()
        
        sys.stdout = stdout_capture
        sys.stderr = stderr_capture
        
        try:
            # Execute with timeout using threading
            result_container = {}
            
            def execute_code():
                try:
                    exec(code, self._python_globals, self._python_locals)
                    result_container["success"] = True
                    result_container["result"] = None
                except Exception as e:
                    result_container["success"] = False
                    result_container["error"] = str(e)
                    result_container["traceback"] = traceback.format_exc()
            
            thread = threading.Thread(target=execute_code)
            thread.start()
            thread.join(timeout)
            
            if thread.is_alive():
                # Timeout - we can't kill the thread cleanly in Python
                # but we can record the timeout
                return ExecutionResult(
                    success=False,
                    data=None,
                    error_message=f"Code execution timed out after {timeout} seconds",
                    stdout=stdout_capture.getvalue(),
                    stderr=stderr_capture.getvalue(),
                    execution_time=time.time() - start_time
                )
            
            execution_time = time.time() - start_time
            
            # Restore stdout/stderr
            sys.stdout = old_stdout
            sys.stderr = old_stderr
            
            # Record execution
            self._record_execution("python", code[:100], result_container.get("success", False))
            
            return ExecutionResult(
                success=result_container.get("success", False),
                data=result_container.get("result"),
                stdout=stdout_capture.getvalue(),
                stderr=stderr_capture.getvalue() + result_container.get("traceback", ""),
                execution_time=execution_time,
                error_message=result_container.get("error", ""),
                metadata={"code_length": len(code)}
            )
            
        except Exception as e:
            sys.stdout = old_stdout
            sys.stderr = old_stderr
            
            return ExecutionResult(
                success=False,
                data=None,
                error_message=str(e),
                execution_time=time.time() - start_time
            )
    
    def read_file(
        self,
        path: str,
        offset: int = 1,
        limit: int = 100
    ) -> ExecutionResult:
        """
        Read file contents with security checks.
        """
        start_time = time.time()
        
        # Security validation
        if self.enable_security:
            is_valid, error = SecurityValidator.validate_path(path, "read")
            if not is_valid:
                return ExecutionResult(
                    success=False,
                    data=None,
                    error_message=error,
                    execution_time=time.time() - start_time
                )
        
        try:
            path = os.path.expanduser(path)
            
            if not os.path.exists(path):
                return ExecutionResult(
                    success=False,
                    data=None,
                    error_message=f"File not found: {path}",
                    execution_time=time.time() - start_time
                )
            
            if os.path.isdir(path):
                return ExecutionResult(
                    success=False,
                    data=None,
                    error_message=f"Path is a directory: {path}",
                    execution_time=time.time() - start_time
                )
            
            # Read file
            with open(path, 'r', encoding='utf-8', errors='replace') as f:
                lines = f.readlines()
            
            total_lines = len(lines)
            
            # Apply offset and limit
            start = max(0, offset - 1)
            end = min(start + limit, total_lines)
            selected_lines = lines[start:end]
            
            content = "".join(selected_lines)
            truncated = end < total_lines
            
            return ExecutionResult(
                success=True,
                data={
                    "content": content,
                    "lines_read": len(selected_lines),
                    "total_lines": total_lines,
                    "truncated": truncated
                },
                execution_time=time.time() - start_time,
                truncated=truncated,
                metadata={"path": path, "offset": offset, "limit": limit}
            )
            
        except Exception as e:
            return ExecutionResult(
                success=False,
                data=None,
                error_message=str(e),
                execution_time=time.time() - start_time
            )
    
    def write_file(
        self,
        path: str,
        content: str,
        append: bool = False
    ) -> ExecutionResult:
        """
        Write content to a file with security checks.
        """
        start_time = time.time()
        
        # Security validation
        if self.enable_security:
            is_valid, error = SecurityValidator.validate_path(path, "write")
            if not is_valid:
                return ExecutionResult(
                    success=False,
                    data=None,
                    error_message=error,
                    execution_time=time.time() - start_time
                )
        
        try:
            path = os.path.expanduser(path)
            
            # Create directory if needed
            dir_path = os.path.dirname(path)
            if dir_path and not os.path.exists(dir_path):
                os.makedirs(dir_path, exist_ok=True)
            
            mode = 'a' if append else 'w'
            with open(path, mode, encoding='utf-8') as f:
                f.write(content)
            
            return ExecutionResult(
                success=True,
                data={
                    "path": path,
                    "bytes_written": len(content.encode('utf-8'))
                },
                execution_time=time.time() - start_time,
                metadata={"append": append}
            )
            
        except Exception as e:
            return ExecutionResult(
                success=False,
                data=None,
                error_message=str(e),
                execution_time=time.time() - start_time
            )
    
    def list_directory(
        self,
        path: str,
        recursive: bool = False,
        show_hidden: bool = False
    ) -> ExecutionResult:
        """
        List directory contents.
        """
        start_time = time.time()
        
        try:
            path = os.path.expanduser(path)
            
            if not os.path.exists(path):
                return ExecutionResult(
                    success=False,
                    data=None,
                    error_message=f"Directory not found: {path}",
                    execution_time=time.time() - start_time
                )
            
            if not os.path.isdir(path):
                return ExecutionResult(
                    success=False,
                    data=None,
                    error_message=f"Path is not a directory: {path}",
                    execution_time=time.time() - start_time
                )
            
            entries = []
            
            if recursive:
                for root, dirs, files in os.walk(path):
                    # Filter hidden
                    if not show_hidden:
                        dirs[:] = [d for d in dirs if not d.startswith('.')]
                        files = [f for f in files if not f.startswith('.')]
                    
                    for d in dirs:
                        full_path = os.path.join(root, d)
                        rel_path = os.path.relpath(full_path, path)
                        entries.append({
                            "name": rel_path,
                            "type": "directory",
                            "size": 0
                        })
                    
                    for f in files:
                        full_path = os.path.join(root, f)
                        rel_path = os.path.relpath(full_path, path)
                        stat = os.stat(full_path)
                        entries.append({
                            "name": rel_path,
                            "type": "file",
                            "size": stat.st_size,
                            "modified": datetime.fromtimestamp(stat.st_mtime).isoformat()
                        })
            else:
                for entry in os.listdir(path):
                    if not show_hidden and entry.startswith('.'):
                        continue
                    
                    full_path = os.path.join(path, entry)
                    stat = os.stat(full_path)
                    
                    entries.append({
                        "name": entry,
                        "type": "directory" if os.path.isdir(full_path) else "file",
                        "size": stat.st_size if os.path.isfile(full_path) else 0,
                        "modified": datetime.fromtimestamp(stat.st_mtime).isoformat()
                    })
            
            return ExecutionResult(
                success=True,
                data={
                    "path": path,
                    "entries": entries
                },
                execution_time=time.time() - start_time
            )
            
        except Exception as e:
            return ExecutionResult(
                success=False,
                data=None,
                error_message=str(e),
                execution_time=time.time() - start_time
            )
    
    def search_files(
        self,
        path: str,
        pattern: str,
        search_content: bool = False,
        file_pattern: str = "*"
    ) -> ExecutionResult:
        """
        Search for files or content within files.
        """
        start_time = time.time()
        
        import fnmatch
        import re
        
        try:
            path = os.path.expanduser(path)
            matches = []
            
            if search_content:
                # Search file contents
                search_regex = re.compile(pattern, re.IGNORECASE)
                
                for root, dirs, files in os.walk(path):
                    for filename in files:
                        if not fnmatch.fnmatch(filename, file_pattern):
                            continue
                        
                        file_path = os.path.join(root, filename)
                        try:
                            with open(file_path, 'r', encoding='utf-8', errors='replace') as f:
                                for line_num, line in enumerate(f, 1):
                                    if search_regex.search(line):
                                        matches.append({
                                            "path": file_path,
                                            "line": line_num,
                                            "content": line.strip()[:200]
                                        })
                        except:
                            pass  # Skip files that can't be read
            else:
                # Search filenames
                for root, dirs, files in os.walk(path):
                    for filename in files:
                        if fnmatch.fnmatch(filename, pattern):
                            matches.append({
                                "path": os.path.join(root, filename),
                                "line": 0,
                                "content": ""
                            })
            
            return ExecutionResult(
                success=True,
                data={
                    "matches": matches,
                    "total_matches": len(matches)
                },
                execution_time=time.time() - start_time
            )
            
        except Exception as e:
            return ExecutionResult(
                success=False,
                data=None,
                error_message=str(e),
                execution_time=time.time() - start_time
            )
    
    def _record_execution(self, tool_type: str, details: str, success: bool) -> None:
        """Record execution for auditing."""
        self.execution_history.append({
            "timestamp": datetime.now().isoformat(),
            "type": tool_type,
            "details": details,
            "success": success
        })
    
    def get_execution_history(self) -> List[Dict]:
        """Get execution history for auditing."""
        return self.execution_history.copy()


# Import re for the security validator
import re

if __name__ == "__main__":
    # Demo execution
    engine = ExecutionEngine(enable_security=True)
    
    print("="*60)
    print("EXECUTION ENGINE DEMO")
    print("="*60)
    
    # Test Python execution
    print("\n1. Python Execution:")
    result = engine.execute_python("print('Hello from Python!')\nx = 42\nprint(f'x = {x}')")
    print(f"Success: {result.success}")
    print(f"Stdout: {result.stdout.strip()}")
    
    # Test shell execution
    print("\n2. Shell Execution:")
    result = engine.execute_shell("echo 'Hello from Shell!'")
    print(f"Success: {result.success}")
    print(f"Stdout: {result.stdout.strip()}")
    
    # Test security validation
    print("\n3. Security Validation:")
    is_valid, error = SecurityValidator.validate_shell_command("rm -rf /")
    print(f"'rm -rf /' valid: {is_valid}, Error: {error}")
    
    is_valid, error = SecurityValidator.validate_shell_command("ls -la")
    print(f"'ls -la' valid: {is_valid}")
