# HelixLLM Tool Use System - Complete Design Document

## Executive Summary

This document describes the complete tool use system designed for HelixLLM, a 1.5B parameter local language model. The system enables small LLMs to effectively use tools for coding tasks through smart prompting, robust parsing, secure execution, and intelligent fallback mechanisms.

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         HelixLLM Tool System                             │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐     │
│  │  User Interface │───→│  Model Prompt   │───→│ Response Parser │     │
│  └─────────────────┘    └─────────────────┘    └─────────────────┘     │
│           │                      │                      │               │
│           ↓                      ↓                      ↓               │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐     │
│  │  Tool Registry  │    │  Security Layer │    │ Execution Engine│     │
│  │  (17 tools)     │    │  (Validation)   │    │  (Sandboxed)    │     │
│  └─────────────────┘    └─────────────────┘    └─────────────────┘     │
│           │                      │                      │               │
│           ↓                      ↓                      ↓               │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐     │
│  │ Fallback Handler│    │ Result Processor│    │ Output Formatter│     │
│  │ (5 strategies)  │    │ (Summarization) │    │ (Token-aware)   │     │
│  └─────────────────┘    └─────────────────┘    └─────────────────┘     │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Tool Registry System (`tool_registry.py`)

**Purpose**: Central registry for all available tools with schema validation.

**Key Features**:
- JSON Schema-based tool definitions
- Dynamic tool registration/unregistration
- Tool categorization (File System, Code Execution, Git, etc.)
- Semantic search for tool discovery
- Version management
- Parameter validation

**Classes**:
- `ToolRegistry`: Main registry class
- `ToolDefinition`: Complete tool schema
- `ParameterSchema`: Individual parameter definition
- `ToolCategory`: Enum for tool categories
- `ToolPermission`: Enum for permission levels

**Usage**:
```python
from tool_registry import ToolRegistry, ToolDefinition, get_registry

registry = get_registry()
tool = registry.get_tool("read_file")
tools = registry.list_tools(category=ToolCategory.FILE_SYSTEM)
```

### 2. Tool Definitions (`tool_definitions.py`)

**Purpose**: Pre-defined tool schemas for common coding tasks.

**Available Tools (17 total)**:

#### File System (5 tools)
- `read_file`: Read file contents with line limits
- `write_file`: Write or append to files
- `list_directory`: List directory contents
- `search_files`: Search files by name or content
- `file_exists`: Check if file exists

#### Code Execution (2 tools)
- `execute_python`: Execute Python code in sandbox
- `execute_shell`: Execute shell commands

#### Git Operations (4 tools)
- `git_status`: Get repository status
- `git_diff`: Show changes
- `git_log`: View commit history
- `git_branch`: List branches

#### Testing & Analysis (4 tools)
- `run_tests`: Run test suites
- `analyze_code`: Static code analysis
- `get_dependencies`: Extract project dependencies
- `calculate_complexity`: Calculate code complexity

#### Web (2 tools)
- `web_search`: Search the web
- `fetch_url`: Fetch URL content

### 3. Function Calling Pipeline (`function_caller.py`)

**Purpose**: Orchestrate function calling optimized for 1.5B models.

**Key Features**:
- Smart prompting with few-shot examples
- Multiple response parsing strategies
- Retry logic with exponential backoff
- Multi-turn conversation support
- Token budget management

**Prompt Engineering Strategy**:

```
=== AVAILABLE TOOLS ===
[Clear, concise tool descriptions]

=== HOW TO USE TOOLS ===
[Exact XML format specification]

=== RULES ===
1. Use tools when helpful
2. Use exact XML format
3. Wait for tool results
4. Don't make up information

=== EXAMPLES ===
[7+ few-shot examples]
```

**Tool Call Format**:
```xml
<function_calls>
<invoke name="TOOL_NAME">
<parameter name="PARAM_NAME">VALUE</parameter>
</invoke>
</function_calls>
```

**Classes**:
- `FunctionCaller`: Main orchestrator
- `ResponseParser`: Extract tool calls from model output
- `PromptTemplate`: Generate optimized prompts
- `ToolCall`: Represent a single tool call
- `ConversationTurn`: Track conversation history

### 4. Execution Engine (`execution_engine.py`)

**Purpose**: Secure, sandboxed execution environment for tools.

**Key Features**:
- Sandboxed execution with resource limits
- Timeout handling
- Security validation
- Result formatting
- Error handling

**Security Measures**:

#### Shell Command Validation
```python
DANGEROUS_PATTERNS = [
    r'rm\s+-rf\s+/',      # Root deletion
    r'>\s*/dev/',          # Device overwrite
    r'mkfs\.',             # Filesystem formatting
    r'dd\s+if=.*of=/dev/', # Direct device write
    r'curl.*\|.*sh',       # Pipe to shell
    r'eval\s*\(',          # Code evaluation
]
```

#### Path Validation
- Blocks access to sensitive paths (`/etc/`, `/proc/`, etc.)
- Configurable allowed paths
- Symlink traversal protection

#### Python Code Validation
- Blocks dangerous imports (`os.system`, `subprocess`)
- Blocks `eval()`, `exec()`, `compile()`
- Blocks `__import__()` tricks

**Resource Limits**:
- CPU time: 30 seconds
- Memory: 512 MB
- File size: 100 MB
- Processes: 10
- Open files: 100

**Classes**:
- `ExecutionEngine`: Main execution class
- `ExecutionResult`: Standardized result format
- `SecurityValidator`: Security checks
- `ResourceLimiter`: Resource management

### 5. Result Processor (`result_processor.py`)

**Purpose**: Process and format tool results for LLM consumption.

**Key Features**:
- Token-aware truncation
- Structure preservation
- Smart summarization
- Format conversion

**Output Formats**:
- `RAW`: Original result
- `JSON`: JSON-formatted
- `MARKDOWN`: Markdown formatted
- `SUMMARY`: Human-readable summary
- `TRUNCATED`: Token-aware truncation

**Specialized Summarizers**:
- File content: Shows line counts and truncation status
- Directory listing: Groups files/directories with sizes
- Search results: Lists matches with context
- Code analysis: Shows issues and metrics
- Test results: Shows pass/fail counts
- Git status: Shows branch and file states

**Classes**:
- `ResultProcessor`: Main processing class
- `ProcessingConfig`: Configuration options
- `OutputFormat`: Enum for formats
- `ErrorEnhancer`: Enhance error messages

### 6. Fallback Strategies (`fallback_strategies.py`)

**Purpose**: Recovery mechanisms when tool calling fails.

**Fallback Types**:

1. **RETRY**: Retry with corrections
   - Missing parameters
   - Invalid types
   - Format errors

2. **ALTERNATIVE**: Suggest alternative tools
   - Unknown tools
   - Permission denied
   - Tool unavailable

3. **DECOMPOSE**: Break into simpler steps
   - Timeout errors
   - Complex operations
   - Resource limits

4. **DIRECT_RESPONSE**: Answer without tools
   - Informational queries
   - Simple questions
   - Tool unnecessary

5. **CLARIFY**: Ask user for clarification
   - Ambiguous requests
   - Missing context
   - Multiple options

**Retry Manager**:
- Exponential backoff
- Max retry limits
- Non-retryable error detection

**Confirmation Manager**:
- Detects destructive operations
- Formats confirmation prompts
- Handles user responses

**Classes**:
- `FallbackStrategies`: Main fallback handler
- `RetryManager`: Retry logic
- `ConfirmationManager`: User confirmation
- `AdaptiveCaller`: Learning from failures

## Integration Module (`helixllm_tools.py`)

**Purpose**: Unified API integrating all components.

**Usage**:
```python
from helixllm_tools import HelixLLMTools, HelixConfig, create_tools

# Define model callback
def my_model(messages):
    # Call your 1.5B model
    return response

# Create tools
tools = create_tools(my_model, working_dir="/project")

# Process messages
result = tools.process_message("Read main.py")
print(result['final_response'])
```

**Configuration**:
```python
config = HelixConfig(
    max_tokens=4096,
    max_tools_in_prompt=20,
    enable_few_shot=True,
    default_timeout=30,
    max_retries=3,
    enable_security=True,
    require_confirmation=True
)
```

## Optimization for 1.5B Models

### Prompt Design Principles

1. **Clear Structure**: Use headers and sections
2. **Few-Shot Examples**: 7+ examples for common tasks
3. **Exact Format**: XML-like tags with precise syntax
4. **Minimal Ambiguity**: Clear rules and constraints
5. **Token Efficiency**: Concise descriptions

### Response Parsing Strategy

1. **Primary Pattern**: Standard XML format
2. **Alternative Patterns**: Handle common mistakes
3. **JSON Fallback**: Support JSON format
4. **Error Recovery**: Graceful degradation

### Error Handling Strategy

1. **Validation Before Execution**: Catch errors early
2. **Retry with Context**: Help model correct mistakes
3. **Alternative Suggestions**: Offer other tools
4. **Graceful Degradation**: Provide partial results

## Security Architecture

### Defense in Depth

```
┌─────────────────────────────────────────┐
│  1. Prompt Security                     │
│     - No sensitive data in prompts      │
├─────────────────────────────────────────┤
│  2. Input Validation                    │
│     - Schema validation                 │
│     - Type checking                     │
├─────────────────────────────────────────┤
│  3. Command Validation                  │
│     - Pattern matching                  │
│     - Blocklist checking                │
├─────────────────────────────────────────┤
│  4. Path Validation                     │
│     - Allowed paths                     │
│     - Sensitive path blocking           │
├─────────────────────────────────────────┤
│  5. Resource Limits                     │
│     - CPU, memory, file size            │
├─────────────────────────────────────────┤
│  6. Execution Sandboxing                │
│     - Isolated environment              │
│     - Timeout handling                  │
├─────────────────────────────────────────┤
│  7. Confirmation for Destructive Ops    │
│     - User approval required            │
└─────────────────────────────────────────┘
```

## Testing

Comprehensive test suite covering:
- Tool registry operations
- Response parsing
- Execution engine
- Security validation
- Result processing
- Fallback strategies
- End-to-end integration

Run tests:
```bash
python test_helixllm_tools.py
```

## Performance Considerations

### Token Budget Management

- System prompt: ~10K characters (~2.5K tokens)
- Tool descriptions: Configurable limit (default 20 tools)
- Result truncation: Configurable (default 2K tokens)
- Conversation history: Managed per-turn

### Optimization Tips

1. **Limit Tools**: Show only relevant tools
2. **Use Summaries**: Enable summarization for large outputs
3. **Set Timeouts**: Prevent hanging operations
4. **Cache Results**: Reuse tool results when possible
5. **Batch Operations**: Combine multiple operations

## Usage Examples

See `examples.py` for comprehensive examples including:
1. Basic setup and usage
2. Custom configuration
3. Multi-turn conversations
4. Error handling
5. Result processing
6. Security features
7. Complete workflows

## File Structure

```
helixllm_tools/
├── __init__.py           # Package exports
├── README.md             # User documentation
├── SYSTEM_DESIGN.md      # This document
├── tool_registry.py      # Tool registration system
├── tool_definitions.py   # Pre-defined tools
├── function_caller.py    # Function calling pipeline
├── execution_engine.py   # Secure execution
├── result_processor.py   # Result formatting
├── fallback_strategies.py # Error recovery
├── helixllm_tools.py     # Main integration module
├── examples.py           # Usage examples
└── test_helixllm_tools.py # Test suite
```

## Future Enhancements

1. **Additional Tools**: Database, API, cloud integrations
2. **Better Parsing**: ML-based response parsing
3. **Context Learning**: Learn from user patterns
4. **Parallel Execution**: Execute independent tools in parallel
5. **Caching Layer**: Cache tool results
6. **Metrics Dashboard**: Track usage and performance
7. **Plugin System**: Easy custom tool registration

## Conclusion

The HelixLLM Tool Use System provides a production-ready framework for enabling 1.5B parameter models to effectively use tools. Through careful prompt engineering, robust parsing, secure execution, and intelligent fallbacks, the system enables small models to perform complex coding tasks with high reliability.

The modular architecture allows for easy extension and customization while maintaining security and performance standards suitable for production deployment.
