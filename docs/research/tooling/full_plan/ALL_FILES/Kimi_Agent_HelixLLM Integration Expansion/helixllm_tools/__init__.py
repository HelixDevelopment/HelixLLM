"""
HelixLLM Tools - Complete Tool Use System for Small LLMs
=========================================================

A comprehensive tool use system optimized for 1.5B parameter language models.

Key Features:
- JSON Schema-based tool definitions
- Smart prompting strategies for small models
- Sandboxed execution environment
- Comprehensive error handling and fallbacks
- Result processing and summarization

Quick Start:
    from helixllm_tools import HelixLLMTools, create_tools
    
    # Define your model callback
    def my_model(messages):
        # Call your 1.5B model here
        return model_response
    
    # Create and initialize tools
    tools = create_tools(my_model, working_dir="/path/to/project")
    
    # Process user messages
    result = tools.process_message("Read my main.py file")
    print(result['final_response'])

Modules:
    tool_registry: Tool registration and discovery
    tool_definitions: Pre-defined tools for coding tasks
    function_caller: Function calling pipeline for small models
    execution_engine: Secure tool execution environment
    result_processor: Result formatting and summarization
    fallback_strategies: Error recovery mechanisms
"""

__version__ = "1.0.0"
__author__ = "HelixLLM Team"

# Main exports
from helixllm_tools import HelixLLMTools, HelixConfig, create_tools

# Component exports
from tool_registry import (
    ToolRegistry,
    ToolDefinition,
    ParameterSchema,
    ToolCategory,
    ToolPermission,
    get_registry,
    tool,
)

from function_caller import (
    FunctionCaller,
    ResponseParser,
    PromptTemplate,
    ToolCall,
    CallStatus,
)

from execution_engine import (
    ExecutionEngine,
    ExecutionResult,
    SecurityValidator,
    ResourceLimiter,
)

from result_processor import (
    ResultProcessor,
    OutputFormat,
    ProcessingConfig,
    ErrorEnhancer,
)

from fallback_strategies import (
    FallbackStrategies,
    RetryManager,
    ConfirmationManager,
    AdaptiveCaller,
    FallbackType,
    FallbackResult,
)

__all__ = [
    # Main
    "HelixLLMTools",
    "HelixConfig",
    "create_tools",
    
    # Registry
    "ToolRegistry",
    "ToolDefinition",
    "ParameterSchema",
    "ToolCategory",
    "ToolPermission",
    "get_registry",
    "tool",
    
    # Function Calling
    "FunctionCaller",
    "ResponseParser",
    "PromptTemplate",
    "ToolCall",
    "CallStatus",
    
    # Execution
    "ExecutionEngine",
    "ExecutionResult",
    "SecurityValidator",
    "ResourceLimiter",
    
    # Processing
    "ResultProcessor",
    "OutputFormat",
    "ProcessingConfig",
    "ErrorEnhancer",
    
    # Fallbacks
    "FallbackStrategies",
    "RetryManager",
    "ConfirmationManager",
    "AdaptiveCaller",
    "FallbackType",
    "FallbackResult",
]
