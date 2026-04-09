"""
HelixLLM Function Calling Pipeline
===================================
Optimized function calling for 1.5B parameter models.

Key Strategies:
1. Structured prompting with clear XML-like tags
2. Few-shot examples in system prompt
3. Response validation and retry logic
4. Multi-turn conversation support
5. Error recovery mechanisms
"""

from typing import Dict, List, Any, Optional, Callable, Tuple, Union
from dataclasses import dataclass, field
from enum import Enum
import json
import re
import xml.etree.ElementTree as ET
from datetime import datetime

from tool_registry import ToolRegistry, get_registry, ToolDefinition, ToolPermission


class CallStatus(str, Enum):
    """Status of a tool call attempt."""
    PENDING = "pending"
    SUCCESS = "success"
    ERROR = "error"
    RETRY = "retry"
    FALLBACK = "fallback"


@dataclass
class ToolCall:
    """Represents a single tool call."""
    tool_name: str
    arguments: Dict[str, Any]
    call_id: str = ""
    status: CallStatus = CallStatus.PENDING
    result: Any = None
    error: Optional[str] = None
    retry_count: int = 0
    timestamp: str = field(default_factory=lambda: datetime.now().isoformat())
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "tool_name": self.tool_name,
            "arguments": self.arguments,
            "call_id": self.call_id,
            "status": self.status.value,
            "result": self.result,
            "error": self.error,
            "retry_count": self.retry_count
        }


@dataclass
class ConversationTurn:
    """A single turn in a tool-using conversation."""
    role: str  # "user", "assistant", "system", "tool"
    content: str
    tool_calls: List[ToolCall] = field(default_factory=list)
    timestamp: str = field(default_factory=lambda: datetime.now().isoformat())


class PromptTemplate:
    """
    Optimized prompt templates for 1.5B models.
    Uses clear structure and few-shot examples.
    """
    
    # System prompt template with tool descriptions
    SYSTEM_PROMPT_TEMPLATE = """You are HelixLLM, an AI assistant with access to tools.
Your task is to help users by using the right tools at the right time.

=== AVAILABLE TOOLS ===
{tool_descriptions}

=== HOW TO USE TOOLS ===
When you need to use a tool, respond ONLY with a tool call in this exact format:

<function_calls>
<invoke name="TOOL_NAME">
<parameter name="PARAMETER_NAME">PARAMETER_VALUE</parameter>
</invoke>
</function_calls>

=== RULES ===
1. Use tools when they help answer the user's question
2. Always use the exact XML format shown above
3. For multiple parameters, include multiple <parameter> tags
4. For string parameters, you can include the value directly
5. For complex values (arrays, objects), use JSON format
6. Wait for tool results before providing your final answer
7. If a tool fails, try an alternative approach
8. Do not make up information - use tools to get facts

=== EXAMPLES ===
{examples}

=== RESPONSE FORMAT ===
- If you need a tool: output ONLY the <function_calls> block
- If you have the answer: provide your response directly
- After receiving tool results: analyze and respond to the user

Remember: Be precise with tool names and parameters."""

    # Few-shot examples for common scenarios
    FEW_SHOT_EXAMPLES = """
Example 1 - Reading a file:
User: What's in my main.py file?
Assistant: <function_calls>
<invoke name="read_file">
<parameter name="path">/home/user/project/main.py</parameter>
</invoke>
</function_calls>

Example 2 - Searching for code:
User: Find all functions named "process_data" in my project
Assistant: <function_calls>
<invoke name="search_files">
<parameter name="path">/home/user/project</parameter>
<parameter name="pattern">def process_data</parameter>
<parameter name="search_content">true</parameter>
</invoke>
</function_calls>

Example 3 - Running Python:
User: Calculate the factorial of 10
Assistant: <function_calls>
<invoke name="execute_python">
<parameter name="code">import math\nresult = math.factorial(10)\nprint(result)</parameter>
</invoke>
</function_calls>

Example 4 - Git status:
User: What's the status of my git repo?
Assistant: <function_calls>
<invoke name="git_status">
<parameter name="path">/home/user/project</parameter>
</invoke>
</function_calls>

Example 5 - Listing directory:
User: Show me the files in my project
Assistant: <function_calls>
<invoke name="list_directory">
<parameter name="path">/home/user/project</parameter>
</invoke>
</function_calls>

Example 6 - Multiple parameters:
User: Search for TODO comments in Python files
Assistant: <function_calls>
<invoke name="search_files">
<parameter name="path">/home/user/project</parameter>
<parameter name="pattern">TODO</parameter>
<parameter name="search_content">true</parameter>
<parameter name="file_pattern">*.py</parameter>
</invoke>
</function_calls>

Example 7 - Complex Python:
User: Analyze this data: [1, 2, 3, 4, 5]
Assistant: <function_calls>
<invoke name="execute_python">
<parameter name="code">data = [1, 2, 3, 4, 5]\nmean = sum(data) / len(data)\nprint(f"Mean: {mean}")</parameter>
</invoke>
</function_calls>"""

    # Error correction prompt
    CORRECTION_PROMPT = """Your previous tool call had an error. Please fix it and try again.

Error: {error}

Original call:
Tool: {tool_name}
Arguments: {arguments}

Remember:
1. Check the tool name is correct
2. Verify all required parameters are included
3. Ensure parameter types are correct
4. Use the exact XML format

Please provide the corrected tool call."""

    # Result summary prompt
    RESULT_SUMMARY_PROMPT = """You received the following tool result:

Tool: {tool_name}
Result: {result}

Please provide a helpful response to the user based on this result.
Summarize key information and explain what was found or accomplished."""

    @classmethod
    def build_system_prompt(
        cls,
        registry: ToolRegistry,
        max_tools: int = 20,
        include_examples: bool = True
    ) -> str:
        """Build the complete system prompt with tool descriptions."""
        tool_descriptions = registry.get_tools_for_prompt(max_tools)
        
        examples = cls.FEW_SHOT_EXAMPLES if include_examples else ""
        
        return cls.SYSTEM_PROMPT_TEMPLATE.format(
            tool_descriptions=tool_descriptions,
            examples=examples
        )
    
    @classmethod
    def build_correction_prompt(cls, tool_call: ToolCall, error: str) -> str:
        """Build a prompt to help the model correct a failed tool call."""
        return cls.CORRECTION_PROMPT.format(
            error=error,
            tool_name=tool_call.tool_name,
            arguments=json.dumps(tool_call.arguments, indent=2)
        )
    
    @classmethod
    def build_result_summary_prompt(cls, tool_call: ToolCall) -> str:
        """Build a prompt to summarize tool results."""
        result_str = json.dumps(tool_call.result, indent=2) if not isinstance(tool_call.result, str) else tool_call.result
        return cls.RESULT_SUMMARY_PROMPT.format(
            tool_name=tool_call.tool_name,
            result=result_str[:2000]  # Limit result size
        )


class ResponseParser:
    """
    Parse model responses to extract tool calls.
    Handles various formats and provides error recovery.
    """
    
    # Patterns for extracting tool calls
    FUNCTION_CALL_PATTERN = re.compile(
        r'<function_calls>\s*<invoke\s+name="([^"]+)">\s*(.*?)</invoke>\s*</function_calls>',
        re.DOTALL | re.IGNORECASE
    )
    
    PARAMETER_PATTERN = re.compile(
        r'<parameter\s+name="([^"]+)">(.*?)</parameter>',
        re.DOTALL | re.IGNORECASE
    )
    
    # Alternative patterns for common mistakes
    ALT_INVOKE_PATTERN = re.compile(
        r'<invoke[^>]*name=["\']([^"\']+)["\'][^>]*>(.*?)</invoke>',
        re.DOTALL | re.IGNORECASE
    )
    
    JSON_CODE_BLOCK_PATTERN = re.compile(
        r'```(?:json)?\s*(\{[^`]*\})\s*```',
        re.DOTALL
    )
    
    @classmethod
    def extract_tool_calls(cls, response: str) -> List[ToolCall]:
        """
        Extract all tool calls from a model response.
        Returns list of ToolCall objects.
        """
        tool_calls = []
        
        # Try primary pattern
        matches = cls.FUNCTION_CALL_PATTERN.findall(response)
        
        for tool_name, params_block in matches:
            arguments = cls._parse_parameters(params_block)
            tool_calls.append(ToolCall(
                tool_name=tool_name.strip(),
                arguments=arguments,
                call_id=f"call_{len(tool_calls)}"
            ))
        
        # If no matches, try alternative patterns
        if not tool_calls:
            tool_calls = cls._try_alternative_parsing(response)
        
        return tool_calls
    
    @classmethod
    def _parse_parameters(cls, params_block: str) -> Dict[str, Any]:
        """Parse parameter tags from an invoke block."""
        arguments = {}
        
        for match in cls.PARAMETER_PATTERN.findall(params_block):
            param_name = match[0].strip()
            param_value = match[1].strip()
            
            # Try to parse as JSON for complex types
            try:
                arguments[param_name] = json.loads(param_value)
            except json.JSONDecodeError:
                # Keep as string
                arguments[param_name] = param_value
        
        return arguments
    
    @classmethod
    def _try_alternative_parsing(cls, response: str) -> List[ToolCall]:
        """Try alternative parsing strategies for malformed responses."""
        tool_calls = []
        
        # Try alternative invoke pattern
        alt_matches = cls.ALT_INVOKE_PATTERN.findall(response)
        for tool_name, params_block in alt_matches:
            arguments = cls._parse_parameters(params_block)
            tool_calls.append(ToolCall(
                tool_name=tool_name.strip(),
                arguments=arguments,
                call_id=f"call_{len(tool_calls)}"
            ))
        
        # Try JSON code block
        if not tool_calls:
            json_matches = cls.JSON_CODE_BLOCK_PATTERN.findall(response)
            for json_str in json_matches:
                try:
                    data = json.loads(json_str)
                    if "name" in data and "arguments" in data:
                        tool_calls.append(ToolCall(
                            tool_name=data["name"],
                            arguments=data["arguments"],
                            call_id=f"call_{len(tool_calls)}"
                        ))
                except json.JSONDecodeError:
                    pass
        
        return tool_calls
    
    @classmethod
    def is_tool_call(cls, response: str) -> bool:
        """Check if a response contains a tool call."""
        return bool(cls.FUNCTION_CALL_PATTERN.search(response))
    
    @classmethod
    def clean_response(cls, response: str) -> str:
        """Remove tool call markup from response for display."""
        cleaned = cls.FUNCTION_CALL_PATTERN.sub('', response)
        return cleaned.strip()


class FunctionCaller:
    """
    Main orchestrator for function calling with 1.5B models.
    
    Features:
    - Prompt building with tool descriptions
    - Response parsing and validation
    - Retry logic for failed calls
    - Multi-turn conversation management
    - Token budget management
    """
    
    def __init__(
        self,
        registry: Optional[ToolRegistry] = None,
        max_retries: int = 3,
        token_budget: int = 4000,
        confirmation_callback: Optional[Callable[[str, Dict], bool]] = None
    ):
        self.registry = registry or get_registry()
        self.max_retries = max_retries
        self.token_budget = token_budget
        self.confirmation_callback = confirmation_callback
        self.conversation_history: List[ConversationTurn] = []
        self.system_prompt = ""
        
        # Statistics
        self.stats = {
            "total_calls": 0,
            "successful_calls": 0,
            "failed_calls": 0,
            "retries": 0,
            "fallbacks": 0
        }
    
    def initialize(self, include_examples: bool = True) -> str:
        """Initialize the caller and return the system prompt."""
        self.system_prompt = PromptTemplate.build_system_prompt(
            self.registry,
            include_examples=include_examples
        )
        return self.system_prompt
    
    def process_user_message(
        self,
        user_message: str,
        model_callback: Callable[[List[Dict]], str]
    ) -> Dict[str, Any]:
        """
        Process a user message and handle any tool calls.
        
        Args:
            user_message: The user's input
            model_callback: Function that takes messages and returns model response
        
        Returns:
            Dict with final_response, tool_calls, and conversation
        """
        # Add user message to history
        self.conversation_history.append(ConversationTurn(
            role="user",
            content=user_message
        ))
        
        # Build messages for model
        messages = self._build_messages()
        
        # Get model response
        response = model_callback(messages)
        
        # Check for tool calls
        tool_calls = ResponseParser.extract_tool_calls(response)
        
        if not tool_calls:
            # No tool calls, return direct response
            self.conversation_history.append(ConversationTurn(
                role="assistant",
                content=response
            ))
            return {
                "final_response": response,
                "tool_calls": [],
                "conversation": self.conversation_history
            }
        
        # Execute tool calls
        executed_calls = []
        for tool_call in tool_calls:
            executed_call = self._execute_with_retry(tool_call, model_callback)
            executed_calls.append(executed_call)
        
        # Build final response
        final_response = self._build_final_response(executed_calls, model_callback)
        
        return {
            "final_response": final_response,
            "tool_calls": executed_calls,
            "conversation": self.conversation_history
        }
    
    def _execute_with_retry(
        self,
        tool_call: ToolCall,
        model_callback: Callable[[List[Dict]], str]
    ) -> ToolCall:
        """Execute a tool call with retry logic."""
        self.stats["total_calls"] += 1
        
        for attempt in range(self.max_retries + 1):
            # Validate the call
            is_valid, error = self.registry.validate_tool_call(
                tool_call.tool_name,
                tool_call.arguments
            )
            
            if not is_valid:
                tool_call.status = CallStatus.ERROR
                tool_call.error = error
                
                if attempt < self.max_retries:
                    # Try to get correction from model
                    self.stats["retries"] += 1
                    tool_call.retry_count += 1
                    
                    correction_prompt = PromptTemplate.build_correction_prompt(
                        tool_call, error
                    )
                    messages = self._build_messages() + [
                        {"role": "user", "content": correction_prompt}
                    ]
                    
                    correction_response = model_callback(messages)
                    new_calls = ResponseParser.extract_tool_calls(correction_response)
                    
                    if new_calls:
                        tool_call = new_calls[0]
                        continue
                
                self.stats["failed_calls"] += 1
                return tool_call
            
            # Check for confirmation on destructive operations
            tool_def = self.registry.get_tool(tool_call.tool_name)
            if tool_def and ToolPermission.DESTRUCTIVE in tool_def.permissions:
                if self.confirmation_callback:
                    confirmed = self.confirmation_callback(
                        tool_call.tool_name,
                        tool_call.arguments
                    )
                    if not confirmed:
                        tool_call.status = CallStatus.ERROR
                        tool_call.error = "Operation cancelled by user"
                        return tool_call
            
            # Execute the tool
            handler = self.registry.get_handler(tool_call.tool_name)
            if handler:
                try:
                    result = handler(**tool_call.arguments)
                    tool_call.result = result
                    tool_call.status = CallStatus.SUCCESS
                    self.stats["successful_calls"] += 1
                except Exception as e:
                    tool_call.status = CallStatus.ERROR
                    tool_call.error = str(e)
                    self.stats["failed_calls"] += 1
            else:
                tool_call.status = CallStatus.ERROR
                tool_call.error = f"No handler found for tool: {tool_call.tool_name}"
                self.stats["failed_calls"] += 1
            
            return tool_call
        
        return tool_call
    
    def _build_final_response(
        self,
        tool_calls: List[ToolCall],
        model_callback: Callable[[List[Dict]], str]
    ) -> str:
        """Build the final response after tool execution."""
        # Add tool results to conversation
        for call in tool_calls:
            self.conversation_history.append(ConversationTurn(
                role="tool",
                content=json.dumps(call.to_dict()),
                tool_calls=[call]
            ))
        
        # Build summary prompt
        results_summary = "\n\n".join([
            f"Tool: {call.tool_name}\nResult: {json.dumps(call.result, indent=2) if call.status == CallStatus.SUCCESS else call.error}"
            for call in tool_calls
        ])
        
        summary_prompt = f"""Based on the following tool results, provide a helpful response to the user:

{results_summary}

Please summarize the results and answer the user's original question."""
        
        messages = self._build_messages() + [
            {"role": "user", "content": summary_prompt}
        ]
        
        final_response = model_callback(messages)
        
        self.conversation_history.append(ConversationTurn(
            role="assistant",
            content=final_response
        ))
        
        return final_response
    
    def _build_messages(self) -> List[Dict[str, str]]:
        """Build message list for model from conversation history."""
        messages = [{"role": "system", "content": self.system_prompt}]
        
        for turn in self.conversation_history:
            if turn.role == "tool":
                # Tool results go as function results
                messages.append({
                    "role": "user",
                    "content": f"Tool result: {turn.content}"
                })
            else:
                messages.append({
                    "role": turn.role,
                    "content": turn.content
                })
        
        return messages
    
    def get_stats(self) -> Dict[str, Any]:
        """Get calling statistics."""
        return self.stats.copy()
    
    def clear_history(self) -> None:
        """Clear conversation history."""
        self.conversation_history = []
    
    def reset_stats(self) -> None:
        """Reset statistics."""
        self.stats = {
            "total_calls": 0,
            "successful_calls": 0,
            "failed_calls": 0,
            "retries": 0,
            "fallbacks": 0
        }


class StreamingFunctionCaller(FunctionCaller):
    """
    Extended caller with streaming support for real-time tool execution.
    """
    
    def process_user_message_streaming(
        self,
        user_message: str,
        model_stream_callback: Callable[[List[Dict]], Any]
    ):
        """
        Process user message with streaming support.
        Yields partial responses and tool call updates.
        """
        # Add user message
        self.conversation_history.append(ConversationTurn(
            role="user",
            content=user_message
        ))
        
        messages = self._build_messages()
        
        # Stream the response
        buffer = ""
        tool_call_detected = False
        
        for chunk in model_stream_callback(messages):
            buffer += chunk
            
            # Check if we've received a complete tool call
            if not tool_call_detected and ResponseParser.is_tool_call(buffer):
                tool_call_detected = True
                yield {"type": "tool_call_detected", "content": buffer}
            
            yield {"type": "chunk", "content": chunk}
        
        # Extract and execute tool calls
        if tool_call_detected:
            tool_calls = ResponseParser.extract_tool_calls(buffer)
            for tool_call in tool_calls:
                yield {"type": "executing_tool", "tool": tool_call.tool_name}
                
                executed = self._execute_with_retry(tool_call, lambda m: "")
                yield {"type": "tool_result", "call": executed}


if __name__ == "__main__":
    # Demo usage
    from tool_definitions import register_all_tools
    
    register_all_tools()
    registry = get_registry()
    
    caller = FunctionCaller(registry)
    system_prompt = caller.initialize()
    
    print("="*60)
    print("SYSTEM PROMPT (truncated)")
    print("="*60)
    print(system_prompt[:2000])
    print("\n... [truncated] ...")
    
    # Test response parsing
    test_response = """I'll read the file for you.

<function_calls>
<invoke name="read_file">
<parameter name="path">/home/user/main.py</parameter>
<parameter name="limit">50</parameter>
</invoke>
</function_calls>"""
    
    print("\n" + "="*60)
    print("TEST RESPONSE PARSING")
    print("="*60)
    print(f"Response: {test_response[:100]}...")
    
    tool_calls = ResponseParser.extract_tool_calls(test_response)
    for call in tool_calls:
        print(f"\nParsed Tool Call:")
        print(f"  Name: {call.tool_name}")
        print(f"  Arguments: {call.arguments}")
