#!/usr/bin/env python3
"""
Orchestrator MCP Integration Module
Handles dynamic tool discovery and registration for the orchestrator

This module bridges the MCP client with the task orchestrator,
enabling dynamic tool discovery, selection, and execution.
"""

import json
import asyncio
from typing import Dict, List, Any, Optional, Callable
from dataclasses import dataclass
from enum import Enum
import re

from mcp_client import MCPClient, MCPTool, ToolResult


class ToolCategory(Enum):
    """Tool categories for organization and selection"""
    SEARCH = "search"
    DATA = "data"
    WEB = "web"
    FILE = "file"
    CODE = "code"
    UTILITY = "utility"
    DATABASE = "database"
    BROWSER = "browser"
    SECURITY = "security"
    COMMUNICATION = "communication"


@dataclass
class RegisteredTool:
    """Tool registered with the orchestrator"""
    name: str
    full_name: str  # server:tool format
    description: str
    category: ToolCategory
    parameters: Dict[str, Any]
    required_params: List[str]
    server_name: str
    handler: Callable
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for LLM context"""
        return {
            "name": self.name,
            "full_name": self.full_name,
            "description": self.description,
            "category": self.category.value,
            "parameters": self.parameters,
            "required": self.required_params
        }
    
    def to_json_schema(self) -> Dict[str, Any]:
        """Convert to JSON schema for function calling"""
        return {
            "type": "function",
            "function": {
                "name": self.full_name.replace(":", "_"),
                "description": self.description,
                "parameters": {
                    "type": "object",
                    "properties": self.parameters,
                    "required": self.required_params
                }
            }
        }


class MCPOrchestratorIntegration:
    """
    Integration layer between MCP client and orchestrator.
    
    Handles:
    - Server connection management
    - Tool discovery and registration
    - Tool categorization
    - Tool selection for tasks
    - Tool execution
    """
    
    def __init__(self, config_path: str):
        self.config_path = config_path
        self.mcp_client = MCPClient(config_path)
        self.registered_tools: Dict[str, RegisteredTool] = {}
        self.tool_categories: Dict[ToolCategory, List[str]] = {
            cat: [] for cat in ToolCategory
        }
        self._initialized = False
        
    async def initialize(self) -> bool:
        """Initialize MCP connections and register all tools"""
        try:
            # Connect to all servers
            results = await self.mcp_client.connect_all()
            
            connected_count = sum(1 for r in results.values() if r)
            total_count = len(results)
            print(f"Connected to {connected_count}/{total_count} MCP servers")
            
            # Register all discovered tools
            for server_name, server in self.mcp_client.servers.items():
                for tool_name, tool in server.tools.items():
                    self._register_tool(server_name, tool)
                    
            self._initialized = True
            return True
            
        except Exception as e:
            print(f"MCP initialization failed: {e}")
            return False
            
    def _register_tool(self, server_name: str, tool: MCPTool):
        """Register a single tool with categorization"""
        full_name = f"{server_name}:{tool.name}"
        
        # Determine category
        category = self._categorize_tool(tool.name, tool.description)
        
        # Create handler
        async def handler(**kwargs) -> ToolResult:
            return await self.mcp_client.call_tool(full_name, kwargs)
            
        registered = RegisteredTool(
            name=tool.name,
            full_name=full_name,
            description=tool.description,
            category=category,
            parameters=tool.parameters,
            required_params=tool.required_params,
            server_name=server_name,
            handler=handler
        )
        
        self.registered_tools[full_name] = registered
        self.registered_tools[tool.name] = registered  # Also register by short name
        self.tool_categories[category].append(full_name)
        
    def _categorize_tool(self, name: str, description: str) -> ToolCategory:
        """Categorize a tool based on name and description"""
        text = f"{name} {description}".lower()
        
        # Security tools
        if any(kw in text for kw in ['security', 'scan', 'vulnerability', 'semgrep']):
            return ToolCategory.SECURITY
            
        # Browser automation
        if any(kw in text for kw in ['browser', 'navigate', 'click', 'screenshot', 'puppeteer', 'page']):
            return ToolCategory.BROWSER
            
        # Database
        if any(kw in text for kw in ['database', 'sql', 'table', 'query db', 'sqlite']):
            return ToolCategory.DATABASE
            
        # File operations
        if any(kw in text for kw in ['file', 'read', 'write', 'directory', 'path', 'folder']):
            return ToolCategory.FILE
            
        # Code operations
        if any(kw in text for kw in ['code', 'lint', 'format', 'execute', 'run', 'script']):
            return ToolCategory.CODE
            
        # Web operations
        if any(kw in text for kw in ['web', 'url', 'http', 'fetch', 'download']):
            return ToolCategory.WEB
            
        # Search operations
        if any(kw in text for kw in ['search', 'find', 'lookup', 'query']):
            if any(kw in text for kw in ['web', 'internet', 'online']):
                return ToolCategory.WEB
            return ToolCategory.SEARCH
            
        # Data operations
        if any(kw in text for kw in ['data', 'json', 'csv', 'format', 'convert', 'transform']):
            return ToolCategory.DATA
            
        # Communication
        if any(kw in text for kw in ['github', 'issue', 'email', 'message']):
            return ToolCategory.COMMUNICATION
            
        return ToolCategory.UTILITY
        
    def get_tools_for_task(self, task_description: str, top_k: int = 5) -> List[RegisteredTool]:
        """
        Get relevant tools for a task description.
        Uses keyword matching for relevance scoring.
        
        Args:
            task_description: Description of the task
            top_k: Maximum number of tools to return
            
        Returns:
            List of relevant tools sorted by relevance
        """
        task_lower = task_description.lower()
        scores: Dict[str, float] = {}
        
        for full_name, tool in self.registered_tools.items():
            if ':' not in full_name:  # Skip short names
                continue
                
            score = 0.0
            text = f"{tool.name} {tool.description}".lower()
            
            # Check for keyword matches
            task_words = set(re.findall(r'\b\w+\b', task_lower))
            tool_words = set(re.findall(r'\b\w+\b', text))
            common = task_words & tool_words
            
            # Weight by word importance (longer words = more specific)
            for word in common:
                if len(word) > 4:
                    score += 1.0
                else:
                    score += 0.3
                    
            # Boost for category match
            category_boosts = {
                ToolCategory.SEARCH: ['search', 'find', 'lookup'],
                ToolCategory.FILE: ['file', 'read', 'write', 'save'],
                ToolCategory.CODE: ['code', 'program', 'script'],
                ToolCategory.DATABASE: ['database', 'sql', 'query'],
                ToolCategory.BROWSER: ['browser', 'web', 'page', 'site'],
                ToolCategory.WEB: ['web', 'url', 'http', 'internet'],
                ToolCategory.SECURITY: ['security', 'scan', 'vulnerability'],
            }
            
            for cat, keywords in category_boosts.items():
                if tool.category == cat:
                    for kw in keywords:
                        if kw in task_lower:
                            score += 2.0
                            break
                            
            if score > 0:
                scores[full_name] = score
                
        # Sort by score and return top tools
        sorted_tools = sorted(scores.items(), key=lambda x: x[1], reverse=True)
        return [self.registered_tools[name] for name, _ in sorted_tools[:top_k]]
        
    def get_tools_by_category(self, category: ToolCategory) -> List[RegisteredTool]:
        """Get all tools in a specific category"""
        return [
            self.registered_tools[name] 
            for name in self.tool_categories[category]
        ]
        
    def get_all_tools_context(self) -> str:
        """Generate context string with all available tools for LLM"""
        lines = ["# Available Tools\n"]
        
        for category in ToolCategory:
            tools = self.get_tools_by_category(category)
            if tools:
                lines.append(f"\n## {category.value.title()} Tools\n")
                for tool in tools:
                    lines.append(f"### {tool.full_name}")
                    lines.append(f"Description: {tool.description}")
                    if tool.parameters:
                        lines.append("Parameters:")
                        for param, info in tool.parameters.items():
                            req = " (required)" if param in tool.required_params else ""
                            param_type = info.get('type', 'any')
                            desc = info.get('description', '')
                            lines.append(f"  - {param} ({param_type}){req}: {desc}")
                    lines.append("")
                    
        return '\n'.join(lines)
        
    def get_tools_json_schema(self) -> List[Dict[str, Any]]:
        """Get all tools as JSON schema for function calling"""
        schemas = []
        seen = set()
        
        for tool in self.registered_tools.values():
            if tool.full_name not in seen and ':' in tool.full_name:
                schemas.append(tool.to_json_schema())
                seen.add(tool.full_name)
                
        return schemas
        
    async def execute_tool(self, tool_name: str, arguments: Dict[str, Any]) -> ToolResult:
        """
        Execute a registered tool.
        
        Args:
            tool_name: Tool name (can be full_name or short name)
            arguments: Tool arguments
            
        Returns:
            Tool execution result
        """
        tool = self.registered_tools.get(tool_name)
        
        if not tool:
            return ToolResult(
                success=False,
                content=[],
                error=f"Tool '{tool_name}' not registered"
            )
            
        # Validate required parameters
        missing = [p for p in tool.required_params if p not in arguments]
        if missing:
            return ToolResult(
                success=False,
                content=[],
                error=f"Missing required parameters: {missing}"
            )
            
        return await tool.handler(**arguments)
        
    async def execute_tool_by_llm_call(self, function_call: Dict[str, Any]) -> ToolResult:
        """
        Execute tool from LLM function call format.
        
        Args:
            function_call: Dict with 'name' and 'arguments' keys
            
        Returns:
            Tool execution result
        """
        name = function_call.get('name', '')
        arguments = function_call.get('arguments', {})
        
        # Convert name format (server_tool -> server:tool)
        if '_' in name and ':' not in name:
            parts = name.split('_', 1)
            if len(parts) == 2:
                name = f"{parts[0]}:{parts[1]}"
                
        return await self.execute_tool(name, arguments)
        
    def get_tool_summary(self) -> Dict[str, Any]:
        """Get summary of registered tools"""
        return {
            "total_tools": len([t for t in self.registered_tools.values() if ':' in t.full_name]),
            "servers": list(self.mcp_client.servers.keys()),
            "categories": {
                cat.value: len(tools) 
                for cat, tools in self.tool_categories.items() 
                if tools
            }
        }
        
    async def close(self):
        """Close all connections"""
        await self.mcp_client.close_all()
        self._initialized = False


class ToolSelector:
    """
    Advanced tool selection logic for the orchestrator.
    
    Uses multiple strategies to select appropriate tools:
    - Keyword matching
    - Category requirements
    - Task type detection
    """
    
    def __init__(self, integration: MCPOrchestratorIntegration):
        self.integration = integration
        
    def select_tools(self, task: Dict[str, Any]) -> List[str]:
        """
        Select tools for a task.
        
        Args:
            task: Task specification with:
                - description: Task description
                - requires: List of requirement keywords
                - output_type: Expected output type
                
        Returns:
            List of tool full_names
        """
        description = task.get("description", "")
        requirements = task.get("requires", [])
        
        selected = []
        
        # Match by requirements
        for req in requirements:
            req_lower = req.lower()
            
            if req_lower in ["search", "web_search"]:
                tools = self.integration.get_tools_by_category(ToolCategory.SEARCH)
                tools.extend(self.integration.get_tools_by_category(ToolCategory.WEB))
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req_lower in ["file_access", "file", "filesystem"]:
                tools = self.integration.get_tools_by_category(ToolCategory.FILE)
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req_lower in ["code_analysis", "code", "analyze_code"]:
                tools = self.integration.get_tools_by_category(ToolCategory.CODE)
                tools.extend(self.integration.get_tools_by_category(ToolCategory.SECURITY))
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req_lower in ["database", "db", "sql"]:
                tools = self.integration.get_tools_by_category(ToolCategory.DATABASE)
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req_lower in ["browser", "web_browser", "automation"]:
                tools = self.integration.get_tools_by_category(ToolCategory.BROWSER)
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req_lower in ["security", "scan", "vulnerability"]:
                tools = self.integration.get_tools_by_category(ToolCategory.SECURITY)
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req_lower in ["data", "transform", "convert"]:
                tools = self.integration.get_tools_by_category(ToolCategory.DATA)
                selected.extend([t.full_name for t in tools[:2]])
                
        # Add tools based on description
        relevant = self.integration.get_tools_for_task(description, top_k=5)
        for tool in relevant:
            if tool.full_name not in selected:
                selected.append(tool.full_name)
                
        return selected[:5]  # Limit to top 5 tools
        
    def create_tool_use_prompt(self, task_description: str, selected_tools: List[str]) -> str:
        """
        Create a prompt for the LLM to use the selected tools.
        
        Args:
            task_description: Original task
            selected_tools: List of selected tool full_names
            
        Returns:
            Prompt string
        """
        lines = [
            f"Task: {task_description}",
            "",
            "You have access to the following tools:",
            ""
        ]
        
        for tool_name in selected_tools:
            tool = self.integration.registered_tools.get(tool_name)
            if tool:
                lines.append(f"Tool: {tool.full_name}")
                lines.append(f"Description: {tool.description}")
                if tool.parameters:
                    lines.append("Parameters:")
                    for param, info in tool.parameters.items():
                        req = " (required)" if param in tool.required_params else ""
                        lines.append(f"  - {param}{req}: {info.get('description', 'No description')}")
                lines.append("")
                
        lines.extend([
            "To use a tool, respond with a function call in this format:",
            "",
            "FUNCTION_CALL:",
            "name: <tool_name>",
            "arguments:",
            "  <param1>: <value1>",
            "  <param2>: <value2>",
            ""
        ])
        
        return '\n'.join(lines)


# Convenience functions for common operations

async def create_mcp_integration(config_path: str) -> MCPOrchestratorIntegration:
    """
    Create and initialize MCP integration.
    
    Args:
        config_path: Path to MCP config file
        
    Returns:
        Initialized MCPOrchestratorIntegration
    """
    integration = MCPOrchestratorIntegration(config_path)
    await integration.initialize()
    return integration


def format_tool_result_for_llm(result: ToolResult, tool_name: str) -> str:
    """
    Format tool result for inclusion in LLM context.
    
    Args:
        result: Tool execution result
        tool_name: Name of the tool that was called
        
    Returns:
        Formatted string
    """
    if result.success:
        text = result.get_text()
        # Truncate if too long
        if len(text) > 2000:
            text = text[:2000] + "... [truncated]"
        return f"Tool '{tool_name}' result:\n{text}"
    else:
        return f"Tool '{tool_name}' failed: {result.error}"


# Example usage
async def orchestrator_example():
    """Example of orchestrator using MCP integration"""
    
    # Initialize integration
    integration = MCPOrchestratorIntegration("mcp_config.json")
    await integration.initialize()
    
    # Print summary
    summary = integration.get_tool_summary()
    print(f"Tool Summary: {json.dumps(summary, indent=2)}")
    
    # Get tools context for LLM
    tools_context = integration.get_all_tools_context()
    print(f"\nTools Context (first 1000 chars):\n{tools_context[:1000]}...")
    
    # Select tools for a task
    selector = ToolSelector(integration)
    
    task = {
        "description": "Search for Python best practices and save to file",
        "requires": ["search", "file_access"]
    }
    
    selected_tools = selector.select_tools(task)
    print(f"\nSelected tools for task: {selected_tools}")
    
    # Create tool use prompt
    prompt = selector.create_tool_use_prompt(task["description"], selected_tools)
    print(f"\nTool use prompt:\n{prompt}")
    
    # Execute a tool
    result = await integration.execute_tool(
        "echo:echo",
        {"message": "Hello from orchestrator!"}
    )
    
    print(f"\nTool result:\n{format_tool_result_for_llm(result, 'echo:echo')}")
    
    # Cleanup
    await integration.close()


if __name__ == "__main__":
    asyncio.run(orchestrator_example())
