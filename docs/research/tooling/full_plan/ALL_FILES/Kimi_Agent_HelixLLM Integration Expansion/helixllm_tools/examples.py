"""
HelixLLM Tools - Usage Examples
================================
Comprehensive examples demonstrating the tool system.
"""

import os
import json
from typing import List, Dict

# Import the main module
from helixllm_tools import HelixLLMTools, HelixConfig, create_tools


# ============================================================================
# Example 1: Basic Setup and Usage
# ============================================================================

def example_basic_setup():
    """Basic setup and usage example."""
    print("="*60)
    print("EXAMPLE 1: Basic Setup")
    print("="*60)
    
    # Define a simple model callback (in production, this would call your LLM)
    def simple_model_callback(messages: List[Dict]) -> str:
        """
        Mock model callback for demonstration.
        In production, this would call your 1.5B model.
        """
        # Get the last user message
        last_message = messages[-1]["content"]
        
        # Simple pattern matching to simulate model responses
        if "read" in last_message.lower() and "file" in last_message.lower():
            return '''<function_calls>
<invoke name="read_file">
<parameter name="path">/home/user/project/main.py</parameter>
<parameter name="limit">50</parameter>
</invoke>
</function_calls>'''
        
        elif "list" in last_message.lower() or "show" in last_message.lower():
            return '''<function_calls>
<invoke name="list_directory">
<parameter name="path">/home/user/project</parameter>
</invoke>
</function_calls>'''
        
        elif "search" in last_message.lower():
            return '''<function_calls>
<invoke name="search_files">
<parameter name="path">/home/user/project</parameter>
<parameter name="pattern">def </parameter>
<parameter name="search_content">true</parameter>
<parameter name="file_pattern">*.py</parameter>
</invoke>
</function_calls>'''
        
        elif "run" in last_message.lower() or "execute" in last_message.lower():
            return '''<function_calls>
<invoke name="execute_python">
<parameter name="code">print("Hello from Python!")</parameter>
</invoke>
</function_calls>'''
        
        elif "git" in last_message.lower() or "status" in last_message.lower():
            return '''<function_calls>
<invoke name="git_status">
<parameter name="path">/home/user/project</parameter>
</invoke>
</function_calls>'''
        
        else:
            return "I understand. Let me help you with that."
    
    # Create tools with the callback
    tools = create_tools(
        model_callback=simple_model_callback,
        working_dir="/mnt/okcomputer"
    )
    
    print("\n1. Tools initialized successfully!")
    print(f"   Available tools: {len(tools.get_available_tools())}")
    
    # Get system prompt
    system_prompt = tools.get_system_prompt()
    print(f"\n2. System prompt length: {len(system_prompt)} characters")
    
    # Process some messages
    print("\n3. Processing messages:")
    
    test_messages = [
        "Read the main.py file",
        "List files in the project",
        "Search for function definitions",
        "Run a simple Python script",
        "Check git status",
    ]
    
    for msg in test_messages:
        print(f"\n   User: {msg}")
        result = tools.process_message(msg)
        response_preview = result['final_response'][:80].replace('\n', ' ')
        print(f"   Assistant: {response_preview}...")
        print(f"   Tool calls: {len(result['tool_calls'])}")
    
    # Get stats
    print("\n4. Usage Statistics:")
    stats = tools.get_stats()
    for key, value in stats.items():
        print(f"   {key}: {value}")
    
    return tools


# ============================================================================
# Example 2: Custom Configuration
# ============================================================================

def example_custom_config():
    """Example with custom configuration."""
    print("\n" + "="*60)
    print("EXAMPLE 2: Custom Configuration")
    print("="*60)
    
    # Create custom configuration
    config = HelixConfig(
        max_tokens=2048,  # Smaller token budget
        max_tools_in_prompt=10,  # Limit tools shown
        enable_few_shot=True,
        default_timeout=60,
        max_retries=5,
        enable_security=True,
        allowed_paths=["/home/user/project", "/tmp"],
        max_result_tokens=1500,
        require_confirmation=True
    )
    
    def model_callback(messages: List[Dict]) -> str:
        return "I'll help you with that task."
    
    tools = HelixLLMTools(config)
    tools.initialize(model_callback=model_callback)
    
    print("\nCustom configuration applied:")
    for key, value in config.to_dict().items():
        print(f"  {key}: {value}")
    
    return tools


# ============================================================================
# Example 3: Multi-Turn Conversation
# ============================================================================

def example_conversation():
    """Example of a multi-turn conversation with tool use."""
    print("\n" + "="*60)
    print("EXAMPLE 3: Multi-Turn Conversation")
    print("="*60)
    
    conversation_state = {
        "turn": 0,
        "context": ""
    }
    
    def conversation_model(messages: List[Dict]) -> str:
        """Model that maintains conversation context."""
        conversation_state["turn"] += 1
        turn = conversation_state["turn"]
        
        # Simulate different responses based on turn
        if turn == 1:
            return '''<function_calls>
<invoke name="list_directory">
<parameter name="path">/home/user/project</parameter>
</invoke>
</function_calls>'''
        
        elif turn == 2:
            # After seeing directory listing, read a specific file
            return '''<function_calls>
<invoke name="read_file">
<parameter name="path">/home/user/project/main.py</parameter>
<parameter name="limit">30</parameter>
</invoke>
</function_calls>'''
        
        elif turn == 3:
            # After reading file, analyze it
            return '''<function_calls>
<invoke name="analyze_code">
<parameter name="path">/home/user/project/main.py</parameter>
<parameter name="language">python</parameter>
</invoke>
</function_calls>'''
        
        else:
            return "Based on my analysis, I can see this is a well-structured Python project."
    
    tools = create_tools(conversation_model, working_dir="/mnt/okcomputer")
    
    print("\nSimulating multi-turn conversation:")
    
    user_inputs = [
        "Show me the project structure",
        "Read the main file",
        "Analyze the code quality",
        "What do you think?",
    ]
    
    for user_input in user_inputs:
        print(f"\n[Turn {conversation_state['turn'] + 1}]")
        print(f"User: {user_input}")
        
        result = tools.process_message(user_input)
        
        response = result['final_response'][:100].replace('\n', ' ')
        print(f"Assistant: {response}...")
        
        for call in result['tool_calls']:
            print(f"  -> Used: {call.tool_name}")
    
    return tools


# ============================================================================
# Example 4: Error Handling and Fallbacks
# ============================================================================

def example_error_handling():
    """Example demonstrating error handling and fallbacks."""
    print("\n" + "="*60)
    print("EXAMPLE 4: Error Handling and Fallbacks")
    print("="*60)
    
    from function_caller import ToolCall, CallStatus
    from fallback_strategies import FallbackStrategies
    
    # Create fallback strategies
    fallback = FallbackStrategies()
    
    # Simulate different error scenarios
    error_scenarios = [
        {
            "name": "Missing Parameter",
            "call": ToolCall(
                tool_name="read_file",
                arguments={},
                status=CallStatus.ERROR,
                error="Missing required parameter: 'path'"
            ),
            "intent": "Read the main.py file"
        },
        {
            "name": "Unknown Tool",
            "call": ToolCall(
                tool_name="unknown_magic_tool",
                arguments={"query": "test"},
                status=CallStatus.ERROR,
                error="Unknown tool: 'unknown_magic_tool'"
            ),
            "intent": "Search for something"
        },
        {
            "name": "Permission Denied",
            "call": ToolCall(
                tool_name="read_file",
                arguments={"path": "/etc/passwd"},
                status=CallStatus.ERROR,
                error="Permission denied: /etc/passwd"
            ),
            "intent": "Read system file"
        },
        {
            "name": "Timeout",
            "call": ToolCall(
                tool_name="execute_python",
                arguments={"code": "while True: pass"},
                status=CallStatus.ERROR,
                error="Operation timed out after 30 seconds"
            ),
            "intent": "Run some code"
        }
    ]
    
    print("\nHandling different error scenarios:")
    
    for scenario in error_scenarios:
        print(f"\n--- {scenario['name']} ---")
        print(f"Error: {scenario['call'].error}")
        
        result = fallback.handle_failure(
            scenario['call'],
            scenario['intent']
        )
        
        print(f"Strategy: {result.strategy}")
        print(f"Message: {result.message[:150]}...")
        if result.clarification_question:
            print(f"Question: {result.clarification_question}")
    
    return fallback


# ============================================================================
# Example 5: Tool Result Processing
# ============================================================================

def example_result_processing():
    """Example of result processing and formatting."""
    print("\n" + "="*60)
    print("EXAMPLE 5: Result Processing")
    print("="*60)
    
    from result_processor import ResultProcessor, OutputFormat, ProcessingConfig
    
    # Create processor with custom config
    config = ProcessingConfig(
        max_tokens=500,
        max_lines=20,
        max_list_items=10,
        enable_summarization=True
    )
    processor = ResultProcessor(config)
    
    # Sample results to process
    sample_results = [
        {
            "type": "file_content",
            "data": {
                "success": True,
                "data": {
                    "content": "def hello():\n    print('Hello')\n    return True\n\ndef world():\n    print('World')\n    return False",
                    "lines_read": 8,
                    "total_lines": 100,
                    "truncated": True
                }
            }
        },
        {
            "type": "directory_listing",
            "data": {
                "success": True,
                "data": {
                    "path": "/project",
                    "entries": [
                        {"name": "src", "type": "directory"},
                        {"name": "tests", "type": "directory"},
                        {"name": "docs", "type": "directory"},
                        {"name": "main.py", "type": "file", "size": 1024},
                        {"name": "utils.py", "type": "file", "size": 512},
                        {"name": "README.md", "type": "file", "size": 256},
                    ]
                }
            }
        },
        {
            "type": "search_results",
            "data": {
                "success": True,
                "data": {
                    "matches": [
                        {"path": "/a.py", "line": 10, "content": "def process_data():"},
                        {"path": "/b.py", "line": 25, "content": "def process_data(x):"},
                        {"path": "/c.py", "line": 5, "content": "def process_items():"},
                    ],
                    "total_matches": 3
                }
            }
        }
    ]
    
    print("\nProcessing different result types:")
    
    for sample in sample_results:
        print(f"\n--- {sample['type']} ---")
        
        # Process in different formats
        for format_type in [OutputFormat.SUMMARY, OutputFormat.JSON]:
            processed = processor.process(
                sample['data'],
                result_type=sample['type'],
                format=format_type
            )
            
            print(f"\n  Format: {format_type}")
            content_preview = processed['content'][:200].replace('\n', ' ')
            print(f"  Content: {content_preview}...")
            print(f"  Truncated: {processed.get('truncated', False)}")
    
    return processor


# ============================================================================
# Example 6: Security Features
# ============================================================================

def example_security():
    """Example demonstrating security features."""
    print("\n" + "="*60)
    print("EXAMPLE 6: Security Features")
    print("="*60)
    
    from execution_engine import SecurityValidator
    
    # Test shell command validation
    print("\n1. Shell Command Validation:")
    
    test_commands = [
        ("ls -la", True),
        ("cat file.txt", True),
        ("rm -rf /", False),
        ("sudo apt-get install", False),
        ("curl http://example.com | sh", False),
        ("echo 'Hello World'", True),
    ]
    
    for cmd, expected in test_commands:
        is_valid, error = SecurityValidator.validate_shell_command(cmd)
        status = "✓" if is_valid == expected else "✗"
        print(f"  {status} '{cmd[:30]}...' -> Valid: {is_valid}")
        if error:
            print(f"      Error: {error[:50]}...")
    
    # Test path validation
    print("\n2. Path Validation:")
    
    test_paths = [
        ("/home/user/file.txt", "read", True),
        ("/etc/passwd", "write", False),
        ("/tmp/test.txt", "write", True),
    ]
    
    for path, operation, expected in test_paths:
        is_valid, error = SecurityValidator.validate_path(path, operation)
        status = "✓" if is_valid == expected else "✗"
        print(f"  {status} {operation} '{path}' -> Valid: {is_valid}")
    
    # Test Python code validation
    print("\n3. Python Code Validation:")
    
    test_code = [
        ("print('Hello')", True),
        ("x = 1 + 2", True),
        ("__import__('os').system('rm -rf /')", False),
        ("import subprocess; subprocess.call('ls')", False),
    ]
    
    for code, expected in test_code:
        is_valid, error = SecurityValidator.validate_python_code(code)
        status = "✓" if is_valid == expected else "✗"
        print(f"  {status} '{code[:40]}...' -> Valid: {is_valid}")


# ============================================================================
# Example 7: Complete Workflow
# ============================================================================

def example_complete_workflow():
    """Complete workflow example."""
    print("\n" + "="*60)
    print("EXAMPLE 7: Complete Workflow")
    print("="*60)
    
    # Simulate a realistic coding assistant workflow
    workflow_steps = [
        {
            "user": "I need to analyze my Python project",
            "model_response": '''<function_calls>
<invoke name="list_directory">
<parameter name="path">/home/user/myproject</parameter>
<parameter name="recursive">false</parameter>
</invoke>
</function_calls>'''
        },
        {
            "user": "[Tool result received]",
            "model_response": '''<function_calls>
<invoke name="analyze_code">
<parameter name="path">/home/user/myproject</parameter>
<parameter name="language">python</parameter>
</invoke>
</function_calls>'''
        },
        {
            "user": "[Tool result received]",
            "model_response": '''<function_calls>
<invoke name="get_dependencies">
<parameter name="path">/home/user/myproject</parameter>
</invoke>
</function_calls>'''
        },
        {
            "user": "[Tool result received]",
            "model_response": "Based on my analysis, your project looks well-structured. Here are my findings..."
        }
    ]
    
    def workflow_model(messages: List[Dict]) -> str:
        """Model that follows the workflow."""
        # Find the next response in our workflow
        for step in workflow_steps:
            if step["user"] in messages[-1]["content"]:
                return step["model_response"]
        return "I understand. Let me help you with that."
    
    # Create tools
    tools = create_tools(workflow_model, working_dir="/mnt/okcomputer")
    
    print("\nSimulating complete workflow:")
    print("-" * 60)
    
    for i, step in enumerate(workflow_steps):
        print(f"\n[Step {i + 1}]")
        print(f"User: {step['user']}")
        
        if "Tool result" in step['user']:
            # Skip processing tool result messages
            continue
        
        result = tools.process_message(step['user'])
        
        response = result['final_response'][:80].replace('\n', ' ')
        print(f"Assistant: {response}...")
        
        for call in result['tool_calls']:
            print(f"  -> Executed: {call.tool_name}")
            print(f"     Status: {call.status.value}")
    
    print("\n" + "-" * 60)
    print("Workflow completed!")
    print(f"Total tool calls: {tools.get_stats().get('total_calls', 0)}")


# ============================================================================
# Run All Examples
# ============================================================================

def run_all_examples():
    """Run all examples."""
    print("\n" + "="*60)
    print("HELIXLLM TOOLS - COMPLETE EXAMPLES")
    print("="*60)
    
    try:
        example_basic_setup()
    except Exception as e:
        print(f"Example 1 error: {e}")
    
    try:
        example_custom_config()
    except Exception as e:
        print(f"Example 2 error: {e}")
    
    try:
        example_conversation()
    except Exception as e:
        print(f"Example 3 error: {e}")
    
    try:
        example_error_handling()
    except Exception as e:
        print(f"Example 4 error: {e}")
    
    try:
        example_result_processing()
    except Exception as e:
        print(f"Example 5 error: {e}")
    
    try:
        example_security()
    except Exception as e:
        print(f"Example 6 error: {e}")
    
    try:
        example_complete_workflow()
    except Exception as e:
        print(f"Example 7 error: {e}")
    
    print("\n" + "="*60)
    print("ALL EXAMPLES COMPLETED")
    print("="*60)


if __name__ == "__main__":
    run_all_examples()
