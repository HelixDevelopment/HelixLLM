"""
HelixLLM Fallback Strategies
=============================
Recovery mechanisms when tool calling fails.

Strategies:
1. Retry with corrections
2. Alternative tool selection
3. Decomposition into simpler steps
4. Direct response without tools
5. User clarification requests
"""

import json
import re
from typing import Dict, Any, List, Optional, Callable, Tuple
from dataclasses import dataclass
from enum import Enum

from tool_registry import ToolRegistry, get_registry, ToolDefinition
from function_caller import ToolCall, CallStatus


class FallbackType(str, Enum):
    """Types of fallback strategies."""
    RETRY = "retry"
    ALTERNATIVE = "alternative"
    DECOMPOSE = "decompose"
    DIRECT_RESPONSE = "direct_response"
    CLARIFY = "clarify"


@dataclass
class FallbackResult:
    """Result of a fallback attempt."""
    success: bool
    strategy: FallbackType
    message: str
    new_calls: List[ToolCall] = None
    direct_response: str = ""
    clarification_question: str = ""


class FallbackStrategies:
    """
    Collection of fallback strategies for failed tool calls.
    """
    
    def __init__(self, registry: Optional[ToolRegistry] = None):
        self.registry = registry or get_registry()
        self.strategies: Dict[FallbackType, Callable] = {
            FallbackType.RETRY: self._retry_strategy,
            FallbackType.ALTERNATIVE: self._alternative_strategy,
            FallbackType.DECOMPOSE: self._decompose_strategy,
            FallbackType.DIRECT_RESPONSE: self._direct_response_strategy,
            FallbackType.CLARIFY: self._clarify_strategy,
        }
    
    def handle_failure(
        self,
        failed_call: ToolCall,
        user_intent: str,
        available_context: Dict[str, Any] = None
    ) -> FallbackResult:
        """
        Handle a failed tool call with appropriate fallback.
        
        Args:
            failed_call: The failed tool call
            user_intent: What the user was trying to accomplish
            available_context: Additional context for decision making
        
        Returns:
            FallbackResult with recovery strategy
        """
        error = failed_call.error or "Unknown error"
        
        # Determine best strategy based on error type
        strategy = self._select_strategy(failed_call, error)
        
        # Execute the strategy
        handler = self.strategies.get(strategy, self._direct_response_strategy)
        return handler(failed_call, user_intent, available_context)
    
    def _select_strategy(
        self,
        failed_call: ToolCall,
        error: str
    ) -> FallbackType:
        """Select the best fallback strategy based on error."""
        error_lower = error.lower()
        
        # Missing required parameter - retry with clarification
        if "missing required" in error_lower:
            return FallbackType.RETRY
        
        # Unknown tool - try alternative
        if "unknown tool" in error_lower or "not found" in error_lower:
            return FallbackType.ALTERNATIVE
        
        # Invalid type - retry with correction
        if "invalid type" in error_lower or "type error" in error_lower:
            return FallbackType.RETRY
        
        # Permission denied - try alternative approach
        if "permission" in error_lower or "access" in error_lower:
            return FallbackType.ALTERNATIVE
        
        # Timeout - decompose into smaller steps
        if "timeout" in error_lower or "timed out" in error_lower:
            return FallbackType.DECOMPOSE
        
        # Tool execution failed - try alternative or clarify
        if "execution" in error_lower or "failed" in error_lower:
            return FallbackType.ALTERNATIVE
        
        # Default to clarification
        return FallbackType.CLARIFY
    
    def _retry_strategy(
        self,
        failed_call: ToolCall,
        user_intent: str,
        context: Dict[str, Any]
    ) -> FallbackResult:
        """
        Strategy: Retry with corrections based on error.
        """
        error = failed_call.error
        tool_def = self.registry.get_tool(failed_call.tool_name)
        
        corrections = {}
        suggestions = []
        
        if "missing required" in error.lower():
            # Extract missing parameter name
            match = re.search(r"'([^']+)'", error)
            if match:
                param_name = match.group(1)
                suggestions.append(f"The parameter '{param_name}' is required but was not provided.")
                
                # Try to infer from context
                if param_name == "path" and "file" in user_intent.lower():
                    suggestions.append("Please specify the file path.")
                elif param_name == "command" and "run" in user_intent.lower():
                    suggestions.append("Please specify the command to run.")
        
        if "invalid type" in error.lower():
            suggestions.append("One or more parameters have the wrong type. Please check the expected types.")
        
        message = f"I encountered an error with the {failed_call.tool_name} tool:\n{error}\n\n"
        message += "To fix this:\n"
        for suggestion in suggestions:
            message += f"- {suggestion}\n"
        
        if tool_def:
            message += f"\nTool usage: {tool_def.description}"
        
        return FallbackResult(
            success=False,
            strategy=FallbackType.RETRY,
            message=message,
            clarification_question="Would you like to try again with the correct parameters?"
        )
    
    def _alternative_strategy(
        self,
        failed_call: ToolCall,
        user_intent: str,
        context: Dict[str, Any]
    ) -> FallbackResult:
        """
        Strategy: Find alternative tools that might work.
        """
        # Search for similar tools
        search_terms = [failed_call.tool_name] + user_intent.split()
        
        alternatives = []
        for term in search_terms:
            matches = self.registry.search_tools(term)
            for name, score in matches[:3]:
                if name != failed_call.tool_name and name not in [a["name"] for a in alternatives]:
                    tool = self.registry.get_tool(name)
                    alternatives.append({
                        "name": name,
                        "score": score,
                        "description": tool.description if tool else ""
                    })
        
        # Sort by score
        alternatives.sort(key=lambda x: x["score"], reverse=True)
        
        message = f"The tool '{failed_call.tool_name}' failed with error:\n{failed_call.error}\n\n"
        
        if alternatives:
            message += "Here are some alternative tools that might help:\n\n"
            for alt in alternatives[:5]:
                message += f"- **{alt['name']}**: {alt['description'][:80]}...\n"
        else:
            message += "I couldn't find alternative tools for this task."
        
        return FallbackResult(
            success=False,
            strategy=FallbackType.ALTERNATIVE,
            message=message,
            clarification_question="Would you like to try one of these alternatives?"
        )
    
    def _decompose_strategy(
        self,
        failed_call: ToolCall,
        user_intent: str,
        context: Dict[str, Any]
    ) -> FallbackResult:
        """
        Strategy: Break down complex operation into simpler steps.
        """
        tool_def = self.registry.get_tool(failed_call.tool_name)
        
        message = f"The operation with '{failed_call.tool_name}' failed:\n{failed_call.error}\n\n"
        message += "This might be because the task is too complex. Let me break it down:\n\n"
        
        # Generate decomposition based on tool type
        if failed_call.tool_name in ["search_files", "analyze_code"]:
            message += "Suggested steps:\n"
            message += "1. First, narrow down the search scope to a specific directory\n"
            message += "2. Use more specific search patterns\n"
            message += "3. Process results in batches\n"
        
        elif failed_call.tool_name in ["execute_python", "execute_shell"]:
            message += "Suggested steps:\n"
            message += "1. Break the code into smaller functions\n"
            message += "2. Test each part separately\n"
            message += "3. Combine results step by step\n"
        
        elif failed_call.tool_name in ["read_file", "write_file"]:
            message += "Suggested steps:\n"
            message += "1. Check if the file exists first\n"
            message += "2. Read/write in smaller chunks\n"
            message += "3. Verify each operation succeeded\n"
        
        else:
            message += "Suggested approach:\n"
            message += "1. Start with a simpler version of the task\n"
            message += "2. Verify intermediate results\n"
            message += "3. Build up to the full solution\n"
        
        return FallbackResult(
            success=False,
            strategy=FallbackType.DECOMPOSE,
            message=message,
            clarification_question="Would you like me to proceed with a simpler approach?"
        )
    
    def _direct_response_strategy(
        self,
        failed_call: ToolCall,
        user_intent: str,
        context: Dict[str, Any]
    ) -> FallbackResult:
        """
        Strategy: Provide a direct response without tools.
        """
        message = f"I wasn't able to use the '{failed_call.tool_name}' tool due to an error:\n{failed_call.error}\n\n"
        
        # Provide helpful information based on intent
        if "read" in user_intent.lower() and "file" in user_intent.lower():
            message += "To read a file, I would need:\n"
            message += "- The full file path (e.g., /home/user/project/main.py)\n"
            message += "- Optionally, line limits for large files\n\n"
            message += "Example: read_file with path='/home/user/file.txt'"
        
        elif "search" in user_intent.lower():
            message += "To search files, I would need:\n"
            message += "- The directory path to search in\n"
            message += "- A search pattern (text or glob like '*.py')\n"
            message += "- Whether to search file contents or just names\n\n"
            message += "Example: search_files with path='/project', pattern='TODO', search_content=true"
        
        elif "run" in user_intent.lower() or "execute" in user_intent.lower():
            message += "To execute code, I would need:\n"
            message += "- The code to run (Python or shell command)\n"
            message += "- Any timeout requirements\n\n"
            message += "Example: execute_python with code='print(2+2)'"
        
        else:
            message += "I can help you with this task, but I'll need more specific information. "
            message += "Could you provide more details about what you'd like to accomplish?"
        
        return FallbackResult(
            success=True,
            strategy=FallbackType.DIRECT_RESPONSE,
            message=message,
            direct_response=message
        )
    
    def _clarify_strategy(
        self,
        failed_call: ToolCall,
        user_intent: str,
        context: Dict[str, Any]
    ) -> FallbackResult:
        """
        Strategy: Ask user for clarification.
        """
        message = f"I encountered an issue while trying to help:\n{failed_call.error}\n\n"
        
        # Generate clarification question based on context
        if "path" in str(failed_call.arguments):
            question = "Could you please provide the full path to the file or directory you're referring to?"
        elif "command" in str(failed_call.arguments):
            question = "Could you clarify what command you'd like me to run?"
        elif "pattern" in str(failed_call.arguments):
            question = "What specific pattern would you like me to search for?"
        else:
            question = "Could you provide more details about what you'd like me to do?"
        
        message += question
        
        return FallbackResult(
            success=False,
            strategy=FallbackType.CLARIFY,
            message=message,
            clarification_question=question
        )


class RetryManager:
    """
    Manages retry logic with exponential backoff.
    """
    
    def __init__(
        self,
        max_retries: int = 3,
        base_delay: float = 1.0,
        max_delay: float = 30.0,
        exponential_base: float = 2.0
    ):
        self.max_retries = max_retries
        self.base_delay = base_delay
        self.max_delay = max_delay
        self.exponential_base = exponential_base
    
    def should_retry(self, error: str, attempt: int) -> bool:
        """Determine if we should retry based on error and attempt count."""
        if attempt >= self.max_retries:
            return False
        
        # Don't retry certain errors
        non_retryable = [
            "permission denied",
            "not found",
            "invalid",
            "unknown tool",
            "cancelled",
        ]
        
        error_lower = error.lower()
        for pattern in non_retryable:
            if pattern in error_lower:
                return False
        
        return True
    
    def get_delay(self, attempt: int) -> float:
        """Calculate delay before next retry."""
        delay = self.base_delay * (self.exponential_base ** attempt)
        return min(delay, self.max_delay)
    
    def get_retry_message(self, attempt: int, error: str) -> str:
        """Generate a message for retry attempt."""
        delay = self.get_delay(attempt)
        return f"Attempt {attempt + 1}/{self.max_retries + 1} failed: {error}. Retrying in {delay:.1f}s..."


class ConfirmationManager:
    """
    Manages user confirmation for destructive operations.
    """
    
    DESTRUCTIVE_KEYWORDS = [
        "delete", "remove", "drop", "truncate",
        "overwrite", "replace", "rm", "del",
        "format", "clean", "purge", "destroy"
    ]
    
    def __init__(self, confirmation_callback: Optional[Callable[[str, Dict], bool]] = None):
        self.confirmation_callback = confirmation_callback
        self.pending_confirmations: Dict[str, Dict] = {}
    
    def requires_confirmation(self, tool_name: str, arguments: Dict) -> bool:
        """Check if an operation requires user confirmation."""
        # Check tool name
        if any(keyword in tool_name.lower() for keyword in self.DESTRUCTIVE_KEYWORDS):
            return True
        
        # Check arguments
        args_str = json.dumps(arguments).lower()
        if any(keyword in args_str for keyword in self.DESTRUCTIVE_KEYWORDS):
            return True
        
        return False
    
    def request_confirmation(
        self,
        tool_name: str,
        arguments: Dict,
        operation_id: str = None
    ) -> Tuple[bool, str]:
        """
        Request confirmation from user.
        
        Returns:
            (confirmed, message)
        """
        operation_id = operation_id or f"op_{len(self.pending_confirmations)}"
        
        # Build confirmation message
        message = f"The following operation requires confirmation:\n\n"
        message += f"Tool: {tool_name}\n"
        message += f"Arguments: {json.dumps(arguments, indent=2)}\n\n"
        message += "This operation may modify or delete data."
        
        if self.confirmation_callback:
            confirmed = self.confirmation_callback(tool_name, arguments)
        else:
            # No callback - auto-confirm for testing (not recommended for production)
            confirmed = False
            message += "\n\n[No confirmation handler configured - operation blocked]"
        
        if confirmed:
            self.pending_confirmations.pop(operation_id, None)
            return True, "Operation confirmed"
        else:
            self.pending_confirmations[operation_id] = {
                "tool": tool_name,
                "arguments": arguments,
                "timestamp": ""
            }
            return False, "Operation cancelled by user"
    
    def format_confirmation_prompt(self, tool_name: str, arguments: Dict) -> str:
        """Format a confirmation prompt for display."""
        lines = [
            "⚠️  CONFIRMATION REQUIRED",
            "",
            f"The '{tool_name}' operation may modify or delete data.",
            "",
            "Operation details:",
            json.dumps(arguments, indent=2),
            "",
            "Do you want to proceed? (yes/no)",
        ]
        return "\n".join(lines)


class AdaptiveCaller:
    """
    Adaptive caller that learns from failures and adjusts strategy.
    """
    
    def __init__(self, registry: Optional[ToolRegistry] = None):
        self.registry = registry or get_registry()
        self.fallback_strategies = FallbackStrategies(registry)
        self.retry_manager = RetryManager()
        self.confirmation_manager = ConfirmationManager()
        
        # Track failure patterns
        self.failure_history: List[Dict] = []
        self.success_patterns: Dict[str, int] = {}
    
    def execute_with_adaptive_fallback(
        self,
        tool_call: ToolCall,
        user_intent: str,
        execute_fn: Callable[[ToolCall], ToolCall]
    ) -> ToolCall:
        """
        Execute a tool call with adaptive fallback handling.
        """
        attempt = 0
        
        while True:
            # Check for confirmation
            if self.confirmation_manager.requires_confirmation(
                tool_call.tool_name, tool_call.arguments
            ):
                confirmed, msg = self.confirmation_manager.request_confirmation(
                    tool_call.tool_name, tool_call.arguments
                )
                if not confirmed:
                    tool_call.status = CallStatus.ERROR
                    tool_call.error = msg
                    return tool_call
            
            # Execute
            result = execute_fn(tool_call)
            
            # Success
            if result.status == CallStatus.SUCCESS:
                self._record_success(tool_call)
                return result
            
            # Failure - decide whether to retry or fallback
            if self.retry_manager.should_retry(result.error or "", attempt):
                attempt += 1
                tool_call.retry_count = attempt
                
                # Try fallback strategy
                fallback = self.fallback_strategies.handle_failure(
                    result, user_intent
                )
                
                if fallback.strategy == FallbackType.RETRY:
                    # Apply corrections and retry
                    continue
                else:
                    # Use fallback result
                    result.error = fallback.message
                    return result
            else:
                # Max retries reached or non-retryable error
                self._record_failure(tool_call, result.error)
                return result
    
    def _record_success(self, tool_call: ToolCall) -> None:
        """Record a successful call pattern."""
        pattern = f"{tool_call.tool_name}:{sorted(tool_call.arguments.keys())}"
        self.success_patterns[pattern] = self.success_patterns.get(pattern, 0) + 1
    
    def _record_failure(self, tool_call: ToolCall, error: str) -> None:
        """Record a failure for analysis."""
        self.failure_history.append({
            "tool": tool_call.tool_name,
            "arguments": tool_call.arguments,
            "error": error,
            "timestamp": ""
        })
    
    def get_common_failures(self) -> List[Dict]:
        """Get most common failure patterns."""
        from collections import Counter
        
        error_counts = Counter(f["error"] for f in self.failure_history)
        return [
            {"error": error, "count": count}
            for error, count in error_counts.most_common(10)
        ]


if __name__ == "__main__":
    # Demo
    from tool_definitions import register_all_tools
    
    register_all_tools()
    registry = get_registry()
    
    fallback = FallbackStrategies(registry)
    
    print("="*60)
    print("FALLBACK STRATEGIES DEMO")
    print("="*60)
    
    # Simulate a failed call
    failed_call = ToolCall(
        tool_name="read_file",
        arguments={},
        status=CallStatus.ERROR,
        error="Missing required parameter: 'path'"
    )
    
    print("\n1. Retry Strategy (Missing Parameter):")
    result = fallback._retry_strategy(
        failed_call,
        "Read the main.py file",
        {}
    )
    print(f"Message: {result.message[:200]}...")
    
    # Simulate unknown tool
    failed_call = ToolCall(
        tool_name="unknown_tool",
        arguments={},
        status=CallStatus.ERROR,
        error="Unknown tool: 'unknown_tool'"
    )
    
    print("\n2. Alternative Strategy (Unknown Tool):")
    result = fallback._alternative_strategy(
        failed_call,
        "Find files in the project",
        {}
    )
    print(f"Message: {result.message[:300]}...")
    
    # Test confirmation manager
    confirmation = ConfirmationManager()
    
    print("\n3. Confirmation Check:")
    print(f"write_file requires confirmation: {confirmation.requires_confirmation('write_file', {})}")
    print(f"read_file requires confirmation: {confirmation.requires_confirmation('read_file', {})}")
