#!/usr/bin/env python3
"""
Custom MCP Server Template using FastMCP
FastMCP is a high-level Python SDK for building MCP servers

Installation:
    pip install fastmcp

Usage:
    # Run with stdio transport (for MCP clients)
    python custom_server.py
    
    # Or run with SSE transport
    python custom_server.py --transport sse --port 8000
"""

from fastmcp import FastMCP, Context
from typing import Optional, List, Dict, Any
import asyncio
import json
import argparse

# Create MCP server instance
mcp = FastMCP(
    name="custom-tools-server",
    instructions="""
    This server provides custom tools for the Light Local LLM system.
    Available tools include data processing, calculations, and utility functions.
    """
)


# ============================================
# Tool Registration Examples
# ============================================

@mcp.tool()
def calculate(expression: str) -> str:
    """
    Safely evaluate a mathematical expression.
    
    Args:
        expression: Mathematical expression (e.g., "2 + 2 * 3")
    
    Returns:
        Result of the calculation
    """
    try:
        # Safe evaluation using ast
        import ast
        import operator
        
        allowed_ops = {
            ast.Add: operator.add,
            ast.Sub: operator.sub,
            ast.Mult: operator.mul,
            ast.Div: operator.truediv,
            ast.Pow: operator.pow,
            ast.USub: operator.neg,
        }
        
        def eval_node(node):
            if isinstance(node, ast.Num):
                return node.n
            elif isinstance(node, ast.Constant):  # Python 3.8+
                return node.value
            elif isinstance(node, ast.BinOp):
                return allowed_ops[type(node.op)](eval_node(node.left), eval_node(node.right))
            elif isinstance(node, ast.UnaryOp):
                return allowed_ops[type(node.op)](eval_node(node.operand))
            else:
                raise ValueError(f"Unsupported operation: {type(node)}")
        
        tree = ast.parse(expression, mode='eval')
        result = eval_node(tree.body)
        return f"Result: {result}"
        
    except Exception as e:
        return f"Error: {str(e)}"


@mcp.tool()
def format_data(data: str, format_type: str = "json") -> str:
    """
    Format data into specified format.
    
    Args:
        data: Input data string
        format_type: Output format (json, yaml, csv)
    
    Returns:
        Formatted data string
    """
    try:
        # Try to parse as JSON first
        parsed = json.loads(data)
        
        if format_type == "json":
            return json.dumps(parsed, indent=2)
        elif format_type == "yaml":
            try:
                import yaml
                return yaml.dump(parsed, default_flow_style=False)
            except ImportError:
                return "Error: PyYAML not installed. Install with: pip install pyyaml"
        elif format_type == "csv":
            if isinstance(parsed, list) and len(parsed) > 0:
                import csv
                import io
                output = io.StringIO()
                if isinstance(parsed[0], dict):
                    writer = csv.DictWriter(output, fieldnames=parsed[0].keys())
                    writer.writeheader()
                    writer.writerows(parsed)
                else:
                    writer = csv.writer(output)
                    writer.writerows(parsed)
                return output.getvalue()
            return "Error: Data must be a list for CSV conversion"
        else:
            return f"Unsupported format: {format_type}"
            
    except json.JSONDecodeError:
        return f"Error: Input is not valid JSON"
    except Exception as e:
        return f"Error: {str(e)}"


@mcp.tool()
async def process_text(text: str, operation: str, ctx: Context) -> str:
    """
    Process text with various operations.
    
    Args:
        text: Input text to process
        operation: Operation to perform (uppercase, lowercase, reverse, count_words, summarize, trim)
        ctx: MCP context for progress reporting
    
    Returns:
        Processed text result
    """
    await ctx.info(f"Processing text with operation: {operation}")
    await ctx.report_progress(0, 100)
    
    try:
        if operation == "uppercase":
            result = text.upper()
        elif operation == "lowercase":
            result = text.lower()
        elif operation == "reverse":
            result = text[::-1]
        elif operation == "count_words":
            words = text.split()
            result = f"Word count: {len(words)}"
        elif operation == "count_chars":
            result = f"Character count: {len(text)} (including spaces: {len(text)}"
        elif operation == "summarize":
            # Simple summarization - first sentence
            sentences = [s.strip() for s in text.split('.') if s.strip()]
            if sentences:
                result = f"Summary ({len(sentences)} sentences): {sentences[0]}."
            else:
                result = "No sentences found to summarize."
        elif operation == "trim":
            result = text.strip()
        elif operation == "remove_extra_spaces":
            result = ' '.join(text.split())
        else:
            result = f"Unknown operation: {operation}. Available: uppercase, lowercase, reverse, count_words, count_chars, summarize, trim, remove_extra_spaces"
        
        await ctx.report_progress(100, 100)
        return result
        
    except Exception as e:
        await ctx.error(f"Error processing text: {e}")
        return f"Error: {str(e)}"


@mcp.tool()
def validate_email(email: str) -> str:
    """
    Validate email address format.
    
    Args:
        email: Email address to validate
    
    Returns:
        Validation result
    """
    import re
    
    pattern = r'^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$'
    
    if re.match(pattern, email):
        return f"'{email}' is a valid email address"
    else:
        return f"'{email}' is NOT a valid email address"


@mcp.tool()
def validate_url(url: str) -> str:
    """
    Validate URL format.
    
    Args:
        url: URL to validate
    
    Returns:
        Validation result
    """
    import re
    
    pattern = r'^https?://(?:[-\w.])+(?:[:\d]+)?(?:/(?:[\w/_.])*(?:\?(?:[\w&=%.])*)?(?:#(?:[\w.])*)?)?$'
    
    if re.match(pattern, url):
        return f"'{url}' is a valid URL"
    else:
        return f"'{url}' is NOT a valid URL"


@mcp.tool()
def generate_id(prefix: str = "id", length: int = 8) -> str:
    """
    Generate a unique identifier.
    
    Args:
        prefix: ID prefix
        length: Random part length (max 32)
    
    Returns:
        Generated unique ID
    """
    import random
    import string
    
    length = min(length, 32)  # Cap at 32 characters
    random_part = ''.join(random.choices(
        string.ascii_lowercase + string.digits, 
        k=length
    ))
    return f"{prefix}_{random_part}"


@mcp.tool()
def hash_string(text: str, algorithm: str = "sha256") -> str:
    """
    Generate hash of a string.
    
    Args:
        text: String to hash
        algorithm: Hash algorithm (md5, sha1, sha256, sha512)
    
    Returns:
        Hash value as hex string
    """
    import hashlib
    
    algorithms = {
        "md5": hashlib.md5,
        "sha1": hashlib.sha1,
        "sha256": hashlib.sha256,
        "sha512": hashlib.sha512
    }
    
    if algorithm not in algorithms:
        return f"Unknown algorithm: {algorithm}. Available: {', '.join(algorithms.keys())}"
    
    hasher = algorithms[algorithm]()
    hasher.update(text.encode('utf-8'))
    return hasher.hexdigest()


@mcp.tool()
def encode_base64(text: str) -> str:
    """
    Encode text to base64.
    
    Args:
        text: Text to encode
    
    Returns:
        Base64 encoded string
    """
    import base64
    return base64.b64encode(text.encode('utf-8')).decode('utf-8')


@mcp.tool()
def decode_base64(encoded: str) -> str:
    """
    Decode base64 to text.
    
    Args:
        encoded: Base64 encoded string
    
    Returns:
        Decoded text
    """
    import base64
    try:
        return base64.b64decode(encoded.encode('utf-8')).decode('utf-8')
    except Exception as e:
        return f"Error decoding: {str(e)}"


@mcp.tool()
def parse_json(json_string: str) -> str:
    """
    Parse and validate JSON string.
    
    Args:
        json_string: JSON string to parse
    
    Returns:
        Formatted JSON or error message
    """
    try:
        parsed = json.loads(json_string)
        return json.dumps(parsed, indent=2)
    except json.JSONDecodeError as e:
        return f"Invalid JSON: {str(e)}"


# ============================================
# Resource Registration Examples
# ============================================

@mcp.resource("config://app")
def get_app_config() -> str:
    """Get application configuration"""
    config = {
        "version": "1.0.0",
        "name": "Custom Tools Server",
        "features": [
            "calculator",
            "text_processor",
            "data_formatter",
            "validator",
            "id_generator",
            "hash_functions",
            "base64_codec",
            "json_parser"
        ],
        "limits": {
            "max_text_length": 10000,
            "max_calculation_depth": 10,
            "max_id_length": 50
        }
    }
    return json.dumps(config, indent=2)


@mcp.resource("docs://usage")
def get_usage_docs() -> str:
    """Get usage documentation"""
    return """
# Custom Tools Server Usage

## Available Tools

### calculate
Evaluate mathematical expressions safely.
Example: `calculate("2 + 2 * 3")` → "Result: 8"

### format_data
Convert data between formats (json, yaml, csv).
Example: `format_data('{\\"key\\": \\"value\\"}', "yaml")`

### process_text
Process text with various operations.
Operations: uppercase, lowercase, reverse, count_words, count_chars, summarize, trim, remove_extra_spaces

### validate_email
Validate email address format.
Example: `validate_email("user@example.com")`

### validate_url
Validate URL format.
Example: `validate_url("https://example.com")`

### generate_id
Generate unique identifiers.
Example: `generate_id("user", 10)` → "user_a3f7k9m2p1"

### hash_string
Generate hash of a string.
Algorithms: md5, sha1, sha256, sha512
Example: `hash_string("hello", "sha256")`

### encode_base64
Encode text to base64.
Example: `encode_base64("hello")`

### decode_base64
Decode base64 to text.
Example: `decode_base64("aGVsbG8=")`

### parse_json
Parse and validate JSON string.
Example: `parse_json('{\\"key\\": \\"value\\"}')`

## Resources

- `config://app` - Application configuration
- `docs://usage` - This documentation
"""


# ============================================
# Prompt Registration Examples
# ============================================

@mcp.prompt()
def code_review_prompt(code: str, language: str = "python") -> str:
    """
    Generate a code review prompt.
    
    Args:
        code: Code to review
        language: Programming language
    """
    return f"""Please review the following {language} code:

```{language}
{code}
```

Consider:
1. Code quality and readability
2. Potential bugs or issues
3. Performance considerations
4. Best practices for {language}
5. Security implications
6. Documentation and comments

Provide specific suggestions for improvement."""


@mcp.prompt()
def debug_prompt(error_message: str, context: str = "") -> str:
    """
    Generate a debugging prompt.
    
    Args:
        error_message: The error message
        context: Additional context about the error
    """
    return f"""Help debug this error:

Error: {error_message}

Context: {context}

Please:
1. Explain what this error means
2. Identify likely causes
3. Suggest specific fixes
4. Provide code examples if applicable"""


@mcp.prompt()
def refactor_prompt(code: str, goal: str = "improve readability") -> str:
    """
    Generate a refactoring prompt.
    
    Args:
        code: Code to refactor
        goal: Refactoring goal
    """
    return f"""Please refactor the following code to {goal}:

```
{code}
```

Requirements:
1. Maintain the same functionality
2. Improve code quality
3. Follow best practices
4. Add comments where needed
5. Explain the changes you made"""


# ============================================
# Main Entry Point
# ============================================

def main():
    parser = argparse.ArgumentParser(description='Custom MCP Server')
    parser.add_argument('--transport', choices=['stdio', 'sse'], default='stdio',
                       help='Transport type (default: stdio)')
    parser.add_argument('--host', default='0.0.0.0',
                       help='Host for SSE transport (default: 0.0.0.0)')
    parser.add_argument('--port', type=int, default=8000,
                       help='Port for SSE transport (default: 8000)')
    
    args = parser.parse_args()
    
    if args.transport == 'stdio':
        # Run with stdio transport (for MCP clients)
        mcp.run(transport='stdio')
    else:
        # Run with SSE transport
        print(f"Starting SSE server on {args.host}:{args.port}")
        mcp.run(transport='sse', host=args.host, port=args.port)


if __name__ == "__main__":
    main()
