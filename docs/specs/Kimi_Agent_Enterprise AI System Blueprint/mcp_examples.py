#!/usr/bin/env python3
"""
MCP Usage Examples
Demonstrates common patterns for using the MCP client and integration
"""

import asyncio
import json
from mcp_client import MCPClient, ToolResult
from orchestrator_integration import (
    MCPOrchestratorIntegration, 
    ToolSelector, 
    format_tool_result_for_llm,
    create_mcp_integration
)


# ============================================
# Basic Usage Examples
# ============================================

async def example_basic_connection():
    """Example: Basic connection to MCP servers"""
    print("=" * 50)
    print("Example: Basic Connection")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    
    # Connect to specific servers
    await client.connect_server("echo")
    await client.connect_server("time")
    
    # List all available tools
    tools = client.list_all_tools()
    print(f"\nAvailable tools ({len(tools)}):")
    for tool in tools:
        print(f"  - {tool.server_name}:{tool.name}")
    
    await client.close_all()


async def example_call_tools():
    """Example: Call various MCP tools"""
    print("\n" + "=" * 50)
    print("Example: Call Tools")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_all()
    
    # Call echo tool
    result = await client.call_tool("echo:echo", {
        "message": "Hello, MCP!"
    })
    print(f"\nEcho result: {result.get_text()}")
    
    # Call time tool
    result = await client.call_tool("time:get_current_time", {
        "timezone": "UTC"
    })
    print(f"Time result: {result.get_text()}")
    
    # Call everything server tools
    result = await client.call_tool("everything:add", {
        "a": 10,
        "b": 20
    })
    print(f"Add result: {result.get_text()}")
    
    await client.close_all()


async def example_error_handling():
    """Example: Error handling"""
    print("\n" + "=" * 50)
    print("Example: Error Handling")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_server("echo")
    
    # Call with missing required parameter
    result = await client.call_tool("echo:echo", {})
    
    if result.success:
        print(f"Success: {result.get_text()}")
    else:
        print(f"Error: {result.error}")
    
    # Call non-existent tool
    result = await client.call_tool("echo:nonexistent", {})
    print(f"Non-existent tool error: {result.error}")
    
    await client.close_all()


# ============================================
# Search and Web Examples
# ============================================

async def example_web_search():
    """Example: Web search workflow"""
    print("\n" + "=" * 50)
    print("Example: Web Search")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_server("duckduckgo")
    
    # Search for information
    result = await client.call_tool("duckduckgo:search", {
        "query": "MCP protocol AI tools",
        "max_results": 3
    })
    
    if result.success:
        print(f"Search results:\n{result.get_text()[:500]}...")
    else:
        print(f"Search failed: {result.error}")
    
    await client.close_all()


async def example_wikipedia():
    """Example: Wikipedia search"""
    print("\n" + "=" * 50)
    print("Example: Wikipedia Search")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_server("wikipedia")
    
    # Search Wikipedia
    result = await client.call_tool("wikipedia:search", {
        "query": "Artificial Intelligence",
        "limit": 3
    })
    
    if result.success:
        print(f"Wikipedia results:\n{result.get_text()[:500]}...")
    else:
        print(f"Search failed: {result.error}")
    
    await client.close_all()


# ============================================
# File Operations Examples
# ============================================

async def example_file_operations():
    """Example: File read/write operations"""
    print("\n" + "=" * 50)
    print("Example: File Operations")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_server("filesystem")
    
    # List directory
    result = await client.call_tool("filesystem:list_directory", {
        "path": "."
    })
    print(f"Directory listing:\n{result.get_text()[:300]}...")
    
    # Write a file
    result = await client.call_tool("filesystem:write_file", {
        "path": "test_mcp.txt",
        "content": "Hello from MCP!\nThis is a test file."
    })
    print(f"Write result: {result.get_text()}")
    
    # Read the file
    result = await client.call_tool("filesystem:read_file", {
        "path": "test_mcp.txt"
    })
    print(f"Read result:\n{result.get_text()}")
    
    # Get file info
    result = await client.call_tool("filesystem:get_file_info", {
        "path": "test_mcp.txt"
    })
    print(f"File info:\n{result.get_text()}")
    
    await client.close_all()


# ============================================
# Code Analysis Examples
# ============================================

async def example_security_scan():
    """Example: Security code scanning"""
    print("\n" + "=" * 50)
    print("Example: Security Scan")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_server("semgrep")
    
    # Sample code with security issue
    code = '''
def authenticate(password):
    # Hardcoded password - security issue!
    if password == "admin123":
        return True
    return False

def process_user_input(user_input):
    # SQL injection vulnerability
    query = "SELECT * FROM users WHERE name = '" + user_input + "'"
    return query
'''
    
    result = await client.call_tool("semgrep:scan_code", {
        "code": code,
        "language": "python"
    })
    
    if result.success:
        print(f"Security scan results:\n{result.get_text()}")
    else:
        print(f"Scan failed: {result.error}")
    
    await client.close_all()


# ============================================
# Browser Automation Examples
# ============================================

async def example_browser_automation():
    """Example: Browser automation workflow"""
    print("\n" + "=" * 50)
    print("Example: Browser Automation")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_server("puppeteer")
    
    # Navigate to a page
    result = await client.call_tool("puppeteer:navigate", {
        "url": "https://example.com",
        "wait_until": "networkidle2"
    })
    print(f"Navigation result: {result.get_text()}")
    
    # Get page content
    result = await client.call_tool("puppeteer:get_content", {})
    print(f"Page content (first 300 chars):\n{result.get_text()[:300]}...")
    
    # Take screenshot
    result = await client.call_tool("puppeteer:screenshot", {
        "full_page": True
    })
    print(f"Screenshot result: {result.get_text()}")
    
    await client.close_all()


# ============================================
# Database Examples
# ============================================

async def example_database_operations():
    """Example: Database queries"""
    print("\n" + "=" * 50)
    print("Example: Database Operations")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_server("sqlite")
    
    # List tables
    result = await client.call_tool("sqlite:list_tables", {})
    print(f"Tables:\n{result.get_text()}")
    
    # Get schema
    result = await client.call_tool("sqlite:get_schema", {})
    print(f"Schema:\n{result.get_text()[:500]}...")
    
    # Execute query
    result = await client.call_tool("sqlite:query", {
        "sql": "SELECT name FROM sqlite_master WHERE type='table'"
    })
    print(f"Query result:\n{result.get_text()}")
    
    await client.close_all()


# ============================================
# Orchestrator Integration Examples
# ============================================

async def example_orchestrator_integration():
    """Example: Full orchestrator integration"""
    print("\n" + "=" * 50)
    print("Example: Orchestrator Integration")
    print("=" * 50)
    
    # Create and initialize integration
    integration = await create_mcp_integration("mcp_config.json")
    
    # Get summary
    summary = integration.get_tool_summary()
    print(f"\nTool Summary:\n{json.dumps(summary, indent=2)}")
    
    # Get tools for a task
    tools = integration.get_tools_for_task(
        "Search for Python tutorials and save results to a file",
        top_k=5
    )
    print(f"\nRelevant tools:")
    for tool in tools:
        print(f"  - {tool.full_name}: {tool.description}")
    
    # Use tool selector
    selector = ToolSelector(integration)
    
    task = {
        "description": "Search for AI news and summarize findings",
        "requires": ["search"],
        "output_type": "text"
    }
    
    selected = selector.select_tools(task)
    print(f"\nSelected tools for task: {selected}")
    
    # Execute a tool
    result = await integration.execute_tool("echo:echo", {
        "message": "Hello from orchestrator!"
    })
    print(f"\nExecution result:\n{format_tool_result_for_llm(result, 'echo:echo')}")
    
    await integration.close()


async def example_tool_selection():
    """Example: Advanced tool selection"""
    print("\n" + "=" * 50)
    print("Example: Advanced Tool Selection")
    print("=" * 50)
    
    integration = await create_mcp_integration("mcp_config.json")
    selector = ToolSelector(integration)
    
    tasks = [
        {
            "description": "Find information about climate change",
            "requires": ["search"]
        },
        {
            "description": "Read configuration file and validate JSON",
            "requires": ["file_access"]
        },
        {
            "description": "Scan code for security vulnerabilities",
            "requires": ["security", "code_analysis"]
        },
        {
            "description": "Query user database for active users",
            "requires": ["database"]
        }
    ]
    
    for task in tasks:
        selected = selector.select_tools(task)
        print(f"\nTask: {task['description']}")
        print(f"Selected tools: {selected}")
    
    await integration.close()


# ============================================
# Complex Workflow Examples
# ============================================

async def example_research_workflow():
    """Example: Research workflow combining multiple tools"""
    print("\n" + "=" * 50)
    print("Example: Research Workflow")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_server("duckduckgo")
    await client.connect_server("filesystem")
    
    # Step 1: Search for information
    print("\n1. Searching for information...")
    search_result = await client.call_tool("duckduckgo:search", {
        "query": "Python asyncio best practices 2024",
        "max_results": 5
    })
    
    if not search_result.success:
        print(f"Search failed: {search_result.error}")
        await client.close_all()
        return
    
    search_content = search_result.get_text()
    print(f"Found {len(search_content)} characters of results")
    
    # Step 2: Save results to file
    print("\n2. Saving results to file...")
    save_result = await client.call_tool("filesystem:write_file", {
        "path": "research_results.txt",
        "content": f"Research: Python asyncio best practices\n\n{search_content}"
    })
    
    if save_result.success:
        print("Results saved successfully")
    else:
        print(f"Save failed: {save_result.error}")
    
    # Step 3: Verify file was created
    print("\n3. Verifying file...")
    file_info = await client.call_tool("filesystem:get_file_info", {
        "path": "research_results.txt"
    })
    print(f"File info: {file_info.get_text()}")
    
    await client.close_all()


async def example_data_processing_pipeline():
    """Example: Data processing pipeline"""
    print("\n" + "=" * 50)
    print("Example: Data Processing Pipeline")
    print("=" * 50)
    
    client = MCPClient("mcp_config.json")
    await client.connect_server("filesystem")
    await client.connect_server("sqlite")
    
    # Step 1: Create sample data file
    sample_data = json.dumps([
        {"name": "Alice", "age": 30, "city": "New York"},
        {"name": "Bob", "age": 25, "city": "San Francisco"},
        {"name": "Charlie", "age": 35, "city": "Chicago"}
    ], indent=2)
    
    await client.call_tool("filesystem:write_file", {
        "path": "data.json",
        "content": sample_data
    })
    
    # Step 2: Read the data
    result = await client.call_tool("filesystem:read_file", {
        "path": "data.json"
    })
    data = json.loads(result.get_text())
    print(f"Loaded {len(data)} records")
    
    # Step 3: Process data (filter by age)
    filtered = [r for r in data if r["age"] >= 30]
    print(f"Filtered to {len(filtered)} records (age >= 30)")
    
    # Step 4: Save processed data
    await client.call_tool("filesystem:write_file", {
        "path": "filtered_data.json",
        "content": json.dumps(filtered, indent=2)
    })
    
    print("Pipeline complete!")
    
    await client.close_all()


# ============================================
# Main Runner
# ============================================

EXAMPLES = {
    "basic": example_basic_connection,
    "call": example_call_tools,
    "error": example_error_handling,
    "search": example_web_search,
    "wiki": example_wikipedia,
    "file": example_file_operations,
    "security": example_security_scan,
    "browser": example_browser_automation,
    "database": example_database_operations,
    "orchestrator": example_orchestrator_integration,
    "selection": example_tool_selection,
    "research": example_research_workflow,
    "pipeline": example_data_processing_pipeline,
}


async def run_all_examples():
    """Run all examples"""
    for name, example_func in EXAMPLES.items():
        try:
            await example_func()
        except Exception as e:
            print(f"\nExample '{name}' failed: {e}")


async def main():
    """Main entry point"""
    import sys
    
    if len(sys.argv) > 1:
        # Run specific example
        example_name = sys.argv[1]
        if example_name in EXAMPLES:
            await EXAMPLES[example_name]()
        else:
            print(f"Unknown example: {example_name}")
            print(f"Available examples: {', '.join(EXAMPLES.keys())}")
    else:
        # Run all examples
        print("Running all examples...")
        print("(Pass example name as argument to run specific example)")
        await run_all_examples()


if __name__ == "__main__":
    asyncio.run(main())
