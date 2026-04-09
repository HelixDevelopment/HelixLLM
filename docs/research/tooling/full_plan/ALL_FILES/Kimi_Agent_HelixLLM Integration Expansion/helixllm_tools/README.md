# HelixLLM Tools - Complete Tool Use System

A comprehensive tool use system optimized for 1.5B parameter language models, enabling small LLMs to effectively use tools for coding tasks.

## Overview

HelixLLM Tools provides a complete framework for enabling tool use in small language models through:

- **Smart Prompting**: Structured prompts with few-shot examples
- **Robust Parsing**: Multiple parsing strategies for tool call extraction
- **Secure Execution**: Sandboxed environment with resource limits
- **Intelligent Fallbacks**: Automatic error recovery and alternative approaches
- **Result Processing**: Smart truncation and summarization

## Installation

```bash
# Clone or copy the helixllm_tools directory to your project
cp -r helixllm_tools /path/to/your/project/

# No external dependencies required for core functionality
# Optional: Install for enhanced features
pip install requests  # For web tools
```

## Quick Start

```python
from helixllm_tools import HelixLLMTools, create_tools

# Define your model callback
def my_model_callback(messages):
    """
    Call your 1.5B model here.
    messages: List of dicts with 'role' and 'content'
    Returns: Model response string
    """
    # Example using a local model
    response = your_model.generate(
        messages,
        system_prompt=tools.get_system_prompt()
    )
    return response

# Create and initialize tools
tools = create_tools(
    model_callback=my_model_callback,
    working_dir="/path/to/your/project"
)

# Process user messages
result = tools.process_message("Read my main.py file")
print(result['final_response'])

# Access tool call details
for call in result['tool_calls']:
    print(f"Tool: {call.tool_name}")
    print(f"Arguments: {call.arguments}")
    print(f"Result: {call.result}")
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        HelixLLMTools                             │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   User Input │→ │ Model Prompt │→ │ Tool Parser  │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│         ↓                 ↓                 ↓                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   Fallback   │  │   Security   │  │   Execute    │          │
│  │   Handler    │  │   Validator  │  │   Engine     │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│         ↓                 ↓                 ↓                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   Result     │  │   Process    │  │   Format     │          │
│  │   Processor  │  │   Output     │  │   Response   │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
└─────────────────────────────────────────────────────────────────┘
```

## Available Tools

### File System
- `read_file` - Read file contents with line limits
- `write_file` - Write or append to files
- `list_directory` - List directory contents
- `search_files` - Search files by name or content
- `file_exists` - Check if a file exists

### Code Execution
- `execute_python` - Execute Python code in sandbox
- `execute_shell` - Execute shell commands

### Git Operations
- `git_status` - Get repository status
- `git_diff` - Show changes
- `git_log` - View commit history
- `git_branch` - List branches

### Testing & Analysis
- `run_tests` - Run test suites
- `analyze_code` - Static code analysis
- `get_dependencies` - Extract project dependencies
- `calculate_complexity` - Calculate code complexity

### Web (Optional)
- `web_search` - Search the web
- `fetch_url` - Fetch URL content

## Configuration

```python
from helixllm_tools import HelixConfig, HelixLLMTools

config = HelixConfig(
    # Model settings
    max_tokens=4096,
    temperature=0.1,
    
    # Tool settings
    max_tools_in_prompt=20,
    enable_few_shot=True,
    
    # Execution settings
    default_timeout=30,
    max_retries=3,
    enable_security=True,
    allowed_paths=["/home/user/project", "/tmp"],
    
    # Result processing
    max_result_tokens=2000,
    enable_summarization=True,
    
    # Confirmation
    require_confirmation=True
)

tools = HelixLLMTools(config)
tools.initialize(model_callback=my_callback)
```

## Prompt Engineering for 1.5B Models

The system uses carefully crafted prompts optimized for small models:

### System Prompt Structure
```
=== AVAILABLE TOOLS ===
[Tool descriptions with parameters]

=== HOW TO USE TOOLS ===
[Exact format with XML tags]

=== RULES ===
[Clear, numbered rules]

=== EXAMPLES ===
[Few-shot examples for common tasks]
```

### Tool Call Format
```xml
<function_calls>
<invoke name="TOOL_NAME">
<parameter name="PARAM_NAME">VALUE</parameter>
</invoke>
</function_calls>
```

## Security Features

### Command Validation
```python
from execution_engine import SecurityValidator

# Block dangerous commands
is_valid, error = SecurityValidator.validate_shell_command("rm -rf /")
# Returns: (False, "Command contains dangerous pattern")

# Allow safe commands
is_valid, error = SecurityValidator.validate_shell_command("ls -la")
# Returns: (True, "")
```

### Path Validation
```python
# Block access to sensitive paths
is_valid, error = SecurityValidator.validate_path("/etc/passwd", "read")
# Returns: (False, "Cannot read system path")
```

### Python Code Validation
```python
# Block dangerous code
is_valid, error = SecurityValidator.validate_python_code(
    "__import__('os').system('rm -rf /')"
)
# Returns: (False, "Code contains potentially dangerous pattern")
```

## Error Handling and Fallbacks

```python
from fallback_strategies import FallbackStrategies

fallback = FallbackStrategies()

# Handle failed tool calls
result = fallback.handle_failure(
    failed_call=tool_call,
    user_intent="Read the main.py file"
)

# Available strategies:
# - RETRY: Retry with corrections
# - ALTERNATIVE: Suggest alternative tools
# - DECOMPOSE: Break into simpler steps
# - DIRECT_RESPONSE: Answer without tools
# - CLARIFY: Ask user for clarification
```

## Result Processing

```python
from result_processor import ResultProcessor, OutputFormat

processor = ResultProcessor()

# Process results in different formats
summary = processor.process(
    result=tool_result,
    result_type="file_content",
    format=OutputFormat.SUMMARY
)

# Available formats:
# - RAW: Original result
# - JSON: JSON-formatted
# - MARKDOWN: Markdown formatted
# - SUMMARY: Human-readable summary
# - TRUNCATED: Token-aware truncation
```

## Advanced Usage

### Custom Tool Registration

```python
from tool_registry import ToolDefinition, ParameterSchema, ToolCategory, get_registry

# Define custom tool
custom_tool = ToolDefinition(
    name="my_custom_tool",
    description="Does something custom",
    category=ToolCategory.SYSTEM,
    parameters=[
        ParameterSchema(
            name="input",
            type="string",
            description="Input to process",
            required=True
        )
    ],
    permissions=[ToolPermission.READONLY],
    tags=["custom"]
)

# Register with handler
registry = get_registry()
registry.register(custom_tool, my_handler_function)
```

### Streaming Support

```python
from function_caller import StreamingFunctionCaller

streamer = StreamingFunctionCaller(registry)

for event in streamer.process_user_message_streaming(user_message, model_stream):
    if event["type"] == "chunk":
        print(event["content"], end="")
    elif event["type"] == "tool_result":
        print(f"\nTool executed: {event['call'].tool_name}")
```

### Multi-Turn Conversations

```python
# Conversation history is maintained automatically
result1 = tools.process_message("List files in the project")
result2 = tools.process_message("Read the main.py file")
result3 = tools.process_message("Analyze the code quality")

# Clear history when needed
tools.clear_history()
```

## Integration Examples

### With Transformers

```python
from transformers import AutoModelForCausalLM, AutoTokenizer
from helixllm_tools import create_tools

# Load your 1.5B model
model = AutoModelForCausalLM.from_pretrained("your-model")
tokenizer = AutoTokenizer.from_pretrained("your-model")

def model_callback(messages):
    # Format messages for your model
    prompt = format_messages(messages)
    inputs = tokenizer(prompt, return_tensors="pt")
    outputs = model.generate(**inputs, max_new_tokens=512)
    return tokenizer.decode(outputs[0])

tools = create_tools(model_callback)
```

### With llama.cpp

```python
from llama_cpp import Llama
from helixllm_tools import create_tools

# Load GGUF model
llm = Llama(model_path="model-1.5b.gguf", n_ctx=4096)

def model_callback(messages):
    # Format for llama.cpp
    prompt = "\n".join([f"{m['role']}: {m['content']}" for m in messages])
    output = llm(prompt, max_tokens=512)
    return output["choices"][0]["text"]

tools = create_tools(model_callback)
```

## Best Practices

### 1. Model-Specific Optimization

```python
# Adjust based on your model's capabilities
config = HelixConfig(
    max_tokens=2048,  # Smaller for weaker models
    max_tools_in_prompt=10,  # Fewer tools to reduce confusion
    enable_few_shot=True,  # Essential for small models
)
```

### 2. Error Recovery

```python
# Always handle potential failures
result = tools.process_message(user_input)

if result['tool_calls']:
    for call in result['tool_calls']:
        if call.status != CallStatus.SUCCESS:
            print(f"Tool failed: {call.error}")
            # Implement fallback logic
```

### 3. Resource Management

```python
# Set appropriate timeouts
config = HelixConfig(
    default_timeout=30,  # For quick operations
)

# For long-running operations
result = execution_engine.execute_shell(
    command="long_running_task",
    timeout=300  # 5 minutes
)
```

## Troubleshooting

### Model Not Making Tool Calls

1. Check system prompt is included
2. Verify few-shot examples are enabled
3. Reduce number of tools shown
4. Simplify tool descriptions

### Tool Calls Malformed

1. Check response parsing with `ResponseParser.extract_tool_calls()`
2. Enable alternative parsing strategies
3. Add more examples to system prompt

### Security Blocks

1. Add allowed paths: `SecurityValidator.add_allowed_path("/your/path")`
2. Disable security for testing: `config.enable_security = False`
3. Review blocked patterns in `SecurityValidator`

## Performance Tips

1. **Limit Tools**: Show only relevant tools (5-10 is ideal)
2. **Use Summaries**: Enable `enable_summarization` for large outputs
3. **Set Timeouts**: Prevent hanging on slow operations
4. **Cache Results**: Reuse tool results when possible

## API Reference

See inline documentation in each module:
- `tool_registry.py` - Tool registration and discovery
- `tool_definitions.py` - Pre-defined tool schemas
- `function_caller.py` - Function calling pipeline
- `execution_engine.py` - Secure execution
- `result_processor.py` - Result formatting
- `fallback_strategies.py` - Error recovery

## License

MIT License - See LICENSE file for details

## Contributing

Contributions welcome! Areas for improvement:
- Additional tool definitions
- Enhanced security features
- Better parsing strategies
- More fallback handlers
- Performance optimizations
