"""
HelixLLM Tool Registry System
===============================
A comprehensive tool registration and discovery system optimized for 1.5B parameter LLMs.

Key Features:
- JSON Schema-based tool definitions
- Dynamic tool registration
- Tool categorization and tagging
- Version control for tools
- Semantic search for tool discovery
"""

from typing import Dict, List, Any, Optional, Callable, Type, Union
from dataclasses import dataclass, field, asdict
from enum import Enum
import json
import re
from datetime import datetime


class ToolCategory(str, Enum):
    """Categories for organizing tools."""
    FILE_SYSTEM = "file_system"
    CODE_EXECUTION = "code_execution"
    GIT = "git"
    WEB = "web"
    ANALYSIS = "analysis"
    TESTING = "testing"
    DATABASE = "database"
    SYSTEM = "system"


class ToolPermission(str, Enum):
    """Permission levels for tools."""
    READONLY = "readonly"
    WRITE = "write"
    EXECUTE = "execute"
    DESTRUCTIVE = "destructive"


@dataclass
class ParameterSchema:
    """Schema for a single tool parameter."""
    name: str
    type: str  # string, integer, number, boolean, array, object
    description: str
    required: bool = True
    default: Any = None
    enum: Optional[List[Any]] = None
    items: Optional[Dict] = None  # For array type
    properties: Optional[Dict] = None  # For object type
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to JSON Schema format."""
        schema = {
            "type": self.type,
            "description": self.description
        }
        if self.enum:
            schema["enum"] = self.enum
        if self.items:
            schema["items"] = self.items
        if self.properties:
            schema["properties"] = self.properties
        if self.default is not None:
            schema["default"] = self.default
        return schema


@dataclass
class ToolDefinition:
    """Complete definition of a tool including schema and metadata."""
    name: str
    description: str
    category: ToolCategory
    parameters: List[ParameterSchema]
    permissions: List[ToolPermission]
    version: str = "1.0.0"
    tags: List[str] = field(default_factory=list)
    examples: List[Dict[str, Any]] = field(default_factory=list)
    returns: Dict[str, Any] = field(default_factory=dict)
    long_running: bool = False
    timeout_seconds: int = 30
    requires_confirmation: bool = False
    
    def to_json_schema(self) -> Dict[str, Any]:
        """Convert to OpenAI-style function schema."""
        required = [p.name for p in self.parameters if p.required]
        properties = {p.name: p.to_dict() for p in self.parameters}
        
        return {
            "type": "function",
            "function": {
                "name": self.name,
                "description": self.description,
                "parameters": {
                    "type": "object",
                    "properties": properties,
                    "required": required
                }
            }
        }
    
    def to_compact_schema(self) -> Dict[str, Any]:
        """Compact schema for small models - minimal but complete."""
        return {
            "name": self.name,
            "desc": self.description[:100] + "..." if len(self.description) > 100 else self.description,
            "params": [
                {
                    "name": p.name,
                    "type": p.type,
                    "req": p.required,
                    "desc": p.description[:50] + "..." if len(p.description) > 50 else p.description
                }
                for p in self.parameters
            ]
        }
    
    def get_example_calls(self) -> List[str]:
        """Generate example function calls for few-shot prompting."""
        examples = []
        for ex in self.examples:
            params = json.dumps(ex.get("arguments", {}), ensure_ascii=False)
            examples.append(f'<function_calls><invoke name="{self.name}"><parameter name="arguments">{params}</parameter></invoke></function_calls>')
        return examples


class ToolRegistry:
    """
    Central registry for all available tools.
    
    Features:
    - Dynamic tool registration
    - Tool discovery by category/tag
    - Semantic search capabilities
    - Version management
    """
    
    def __init__(self):
        self._tools: Dict[str, ToolDefinition] = {}
        self._handlers: Dict[str, Callable] = {}
        self._categories: Dict[ToolCategory, List[str]] = {cat: [] for cat in ToolCategory}
        self._tags: Dict[str, List[str]] = {}
    
    def register(
        self,
        definition: ToolDefinition,
        handler: Callable,
        override: bool = False
    ) -> None:
        """
        Register a new tool with its handler.
        
        Args:
            definition: ToolDefinition with complete schema
            handler: Callable that executes the tool
            override: Whether to override existing tool with same name
        """
        if definition.name in self._tools and not override:
            raise ValueError(f"Tool '{definition.name}' already registered. Use override=True to replace.")
        
        self._tools[definition.name] = definition
        self._handlers[definition.name] = handler
        
        # Update category index
        if definition.name not in self._categories[definition.category]:
            self._categories[definition.category].append(definition.name)
        
        # Update tag index
        for tag in definition.tags:
            if tag not in self._tags:
                self._tags[tag] = []
            if definition.name not in self._tags[tag]:
                self._tags[tag].append(definition.name)
    
    def unregister(self, name: str) -> None:
        """Remove a tool from the registry."""
        if name not in self._tools:
            raise ValueError(f"Tool '{name}' not found in registry.")
        
        tool = self._tools[name]
        
        # Remove from category
        if name in self._categories[tool.category]:
            self._categories[tool.category].remove(name)
        
        # Remove from tags
        for tag in tool.tags:
            if name in self._tags.get(tag, []):
                self._tags[tag].remove(name)
        
        del self._tools[name]
        del self._handlers[name]
    
    def get_tool(self, name: str) -> Optional[ToolDefinition]:
        """Get tool definition by name."""
        return self._tools.get(name)
    
    def get_handler(self, name: str) -> Optional[Callable]:
        """Get tool handler by name."""
        return self._handlers.get(name)
    
    def list_tools(
        self,
        category: Optional[ToolCategory] = None,
        tag: Optional[str] = None,
        permission: Optional[ToolPermission] = None
    ) -> List[str]:
        """
        List available tools with optional filtering.
        
        Args:
            category: Filter by tool category
            tag: Filter by tag
            permission: Filter by required permission
        
        Returns:
            List of tool names matching filters
        """
        if category:
            return self._categories.get(category, [])
        
        if tag:
            return self._tags.get(tag, [])
        
        if permission:
            return [
                name for name, tool in self._tools.items()
                if permission in tool.permissions
            ]
        
        return list(self._tools.keys())
    
    def search_tools(self, query: str) -> List[tuple]:
        """
        Search tools by name, description, or tags.
        Returns list of (name, score) tuples sorted by relevance.
        """
        query_lower = query.lower()
        results = []
        
        for name, tool in self._tools.items():
            score = 0
            
            # Exact name match
            if query_lower == name.lower():
                score += 100
            # Partial name match
            elif query_lower in name.lower():
                score += 50
            
            # Description match
            if query_lower in tool.description.lower():
                score += 25
            
            # Tag match
            for tag in tool.tags:
                if query_lower in tag.lower():
                    score += 30
            
            # Category match
            if query_lower in tool.category.value.lower():
                score += 20
            
            if score > 0:
                results.append((name, score))
        
        return sorted(results, key=lambda x: x[1], reverse=True)
    
    def get_all_schemas(self, compact: bool = False) -> List[Dict[str, Any]]:
        """Get schemas for all registered tools."""
        if compact:
            return [tool.to_compact_schema() for tool in self._tools.values()]
        return [tool.to_json_schema() for tool in self._tools.values()]
    
    def get_tools_for_prompt(self, max_tools: int = 20) -> str:
        """
        Generate a formatted tool description for system prompts.
        Optimized for small models with clear formatting.
        """
        lines = ["AVAILABLE TOOLS:"]
        lines.append("=" * 60)
        
        for i, (name, tool) in enumerate(self._tools.items()):
            if i >= max_tools:
                lines.append(f"\n... and {len(self._tools) - max_tools} more tools")
                break
            
            lines.append(f"\n{name}")
            lines.append(f"  Description: {tool.description}")
            lines.append(f"  Category: {tool.category.value}")
            
            if tool.parameters:
                lines.append("  Parameters:")
                for param in tool.parameters:
                    req = "(required)" if param.required else "(optional)"
                    default = f" = {param.default}" if param.default is not None else ""
                    lines.append(f"    - {param.name}: {param.type} {req}{default}")
                    lines.append(f"      {param.description}")
            
            if tool.examples:
                lines.append("  Example:")
                ex = tool.examples[0]
                args = json.dumps(ex.get("arguments", {}), indent=2)
                lines.append(f"    Arguments: {args}")
        
        return "\n".join(lines)
    
    def get_tool_by_category(self, category: ToolCategory) -> List[ToolDefinition]:
        """Get all tools in a specific category."""
        return [
            self._tools[name]
            for name in self._categories.get(category, [])
            if name in self._tools
        ]
    
    def validate_tool_call(self, name: str, arguments: Dict[str, Any]) -> tuple:
        """
        Validate a tool call against its schema.
        
        Returns:
            (is_valid: bool, error_message: Optional[str])
        """
        tool = self._tools.get(name)
        if not tool:
            return False, f"Unknown tool: '{name}'"
        
        # Check required parameters
        for param in tool.parameters:
            if param.required and param.name not in arguments:
                return False, f"Missing required parameter: '{param.name}' for tool '{name}'"
        
        # Check parameter types
        for param_name, value in arguments.items():
            matching_params = [p for p in tool.parameters if p.name == param_name]
            if not matching_params:
                return False, f"Unknown parameter: '{param_name}' for tool '{name}'"
            
            param = matching_params[0]
            
            # Type validation
            type_valid = self._validate_type(value, param.type)
            if not type_valid:
                return False, f"Invalid type for parameter '{param_name}': expected {param.type}, got {type(value).__name__}"
            
            # Enum validation
            if param.enum and value not in param.enum:
                return False, f"Invalid value for '{param_name}': must be one of {param.enum}"
        
        return True, None
    
    def _validate_type(self, value: Any, expected_type: str) -> bool:
        """Validate a value against an expected JSON Schema type."""
        type_map = {
            "string": str,
            "integer": int,
            "number": (int, float),
            "boolean": bool,
            "array": list,
            "object": dict
        }
        
        expected = type_map.get(expected_type)
        if expected is None:
            return True  # Unknown type, allow it
        
        return isinstance(value, expected)
    
    def get_stats(self) -> Dict[str, Any]:
        """Get registry statistics."""
        return {
            "total_tools": len(self._tools),
            "by_category": {
                cat.value: len(tools)
                for cat, tools in self._categories.items()
            },
            "tags": list(self._tags.keys()),
            "destructive_tools": [
                name for name, tool in self._tools.items()
                if ToolPermission.DESTRUCTIVE in tool.permissions
            ]
        }


# Global registry instance
_registry: Optional[ToolRegistry] = None


def get_registry() -> ToolRegistry:
    """Get or create the global tool registry."""
    global _registry
    if _registry is None:
        _registry = ToolRegistry()
    return _registry


def reset_registry() -> None:
    """Reset the global registry (useful for testing)."""
    global _registry
    _registry = ToolRegistry()


# Decorator for easy tool registration
def tool(
    description: str,
    category: ToolCategory = ToolCategory.SYSTEM,
    permissions: Optional[List[ToolPermission]] = None,
    version: str = "1.0.0",
    tags: Optional[List[str]] = None,
    examples: Optional[List[Dict]] = None,
    timeout: int = 30,
    requires_confirmation: bool = False
):
    """
    Decorator to register a function as a tool.
    
    Example:
        @tool(
            description="Read a file",
            category=ToolCategory.FILE_SYSTEM,
            permissions=[ToolPermission.READONLY]
        )
        def read_file(path: str) -> str:
            ...
    """
    def decorator(func: Callable) -> Callable:
        # Extract parameters from function signature
        import inspect
        sig = inspect.signature(func)
        
        parameters = []
        for param_name, param in sig.parameters.items():
            param_type = "string"  # Default
            if param.annotation != inspect.Parameter.empty:
                if param.annotation == int:
                    param_type = "integer"
                elif param.annotation == float:
                    param_type = "number"
                elif param.annotation == bool:
                    param_type = "boolean"
                elif param.annotation == list:
                    param_type = "array"
                elif param.annotation == dict:
                    param_type = "object"
            
            param_schema = ParameterSchema(
                name=param_name,
                type=param_type,
                description=f"Parameter: {param_name}",
                required=param.default == inspect.Parameter.empty,
                default=param.default if param.default != inspect.Parameter.empty else None
            )
            parameters.append(param_schema)
        
        definition = ToolDefinition(
            name=func.__name__,
            description=description,
            category=category,
            parameters=parameters,
            permissions=permissions or [ToolPermission.READONLY],
            version=version,
            tags=tags or [],
            examples=examples or [],
            timeout_seconds=timeout,
            requires_confirmation=requires_confirmation
        )
        
        registry = get_registry()
        registry.register(definition, func)
        
        return func
    return decorator


if __name__ == "__main__":
    # Example usage
    registry = ToolRegistry()
    
    # Create a sample tool definition
    read_file_def = ToolDefinition(
        name="read_file",
        description="Read the contents of a file at the specified path",
        category=ToolCategory.FILE_SYSTEM,
        parameters=[
            ParameterSchema(
                name="path",
                type="string",
                description="Absolute path to the file to read",
                required=True
            ),
            ParameterSchema(
                name="limit",
                type="integer",
                description="Maximum number of lines to read",
                required=False,
                default=100
            )
        ],
        permissions=[ToolPermission.READONLY],
        tags=["file", "read"],
        examples=[
            {"arguments": {"path": "/home/user/file.txt", "limit": 50}}
        ]
    )
    
    # Register with a handler
    def read_file_handler(path: str, limit: int = 100) -> str:
        return f"Contents of {path}"
    
    registry.register(read_file_def, read_file_handler)
    
    print("Registry Stats:")
    print(json.dumps(registry.get_stats(), indent=2))
    print("\nTool Schemas:")
    print(json.dumps(registry.get_all_schemas(compact=True), indent=2))
