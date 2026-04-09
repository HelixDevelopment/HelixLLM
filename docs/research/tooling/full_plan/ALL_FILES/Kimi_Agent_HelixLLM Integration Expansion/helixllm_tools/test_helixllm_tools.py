"""
HelixLLM Tools - Test Suite
============================
Comprehensive tests for the tool system.
"""

import os
import sys
import json
import tempfile
import unittest
from unittest.mock import Mock, patch, MagicMock

# Import all modules
from tool_registry import (
    ToolRegistry, ToolDefinition, ParameterSchema,
    ToolCategory, ToolPermission, get_registry, reset_registry
)
from tool_definitions import create_file_system_tools, register_all_tools
from function_caller import (
    FunctionCaller, ResponseParser, PromptTemplate,
    ToolCall, CallStatus, ConversationTurn
)
from execution_engine import (
    ExecutionEngine, ExecutionResult,
    SecurityValidator, ResourceLimiter
)
from result_processor import ResultProcessor, OutputFormat, ProcessingConfig
from fallback_strategies import (
    FallbackStrategies, RetryManager,
    ConfirmationManager, FallbackType
)
from helixllm_tools import HelixLLMTools, HelixConfig, create_tools


class TestToolRegistry(unittest.TestCase):
    """Test tool registry functionality."""
    
    def setUp(self):
        reset_registry()
        self.registry = get_registry()
    
    def test_register_tool(self):
        """Test basic tool registration."""
        tool_def = ToolDefinition(
            name="test_tool",
            description="A test tool",
            category=ToolCategory.SYSTEM,
            parameters=[],
            permissions=[ToolPermission.READONLY]
        )
        
        handler = Mock(return_value="result")
        self.registry.register(tool_def, handler)
        
        self.assertIn("test_tool", self.registry.list_tools())
        self.assertIsNotNone(self.registry.get_tool("test_tool"))
    
    def test_unregister_tool(self):
        """Test tool unregistration."""
        tool_def = ToolDefinition(
            name="test_tool",
            description="A test tool",
            category=ToolCategory.SYSTEM,
            parameters=[],
            permissions=[ToolPermission.READONLY]
        )
        
        self.registry.register(tool_def, Mock())
        self.registry.unregister("test_tool")
        
        self.assertNotIn("test_tool", self.registry.list_tools())
    
    def test_validate_tool_call(self):
        """Test tool call validation."""
        tool_def = ToolDefinition(
            name="test_tool",
            description="A test tool",
            category=ToolCategory.SYSTEM,
            parameters=[
                ParameterSchema(
                    name="required_param",
                    type="string",
                    description="Required parameter",
                    required=True
                ),
                ParameterSchema(
                    name="optional_param",
                    type="integer",
                    description="Optional parameter",
                    required=False,
                    default=42
                )
            ],
            permissions=[ToolPermission.READONLY]
        )
        
        self.registry.register(tool_def, Mock())
        
        # Valid call
        is_valid, error = self.registry.validate_tool_call(
            "test_tool",
            {"required_param": "value"}
        )
        self.assertTrue(is_valid)
        self.assertIsNone(error)
        
        # Missing required parameter
        is_valid, error = self.registry.validate_tool_call(
            "test_tool",
            {}
        )
        self.assertFalse(is_valid)
        self.assertIn("Missing required", error)
    
    def test_search_tools(self):
        """Test tool search functionality."""
        tool_def = ToolDefinition(
            name="search_test",
            description="Search for files",
            category=ToolCategory.FILE_SYSTEM,
            parameters=[],
            permissions=[ToolPermission.READONLY],
            tags=["search", "find"]
        )
        
        self.registry.register(tool_def, Mock())
        
        results = self.registry.search_tools("search")
        self.assertTrue(len(results) > 0)
        self.assertEqual(results[0][0], "search_test")
    
    def test_get_stats(self):
        """Test registry statistics."""
        register_all_tools()
        stats = self.registry.get_stats()
        
        self.assertIn("total_tools", stats)
        self.assertIn("by_category", stats)
        self.assertGreater(stats["total_tools"], 0)


class TestResponseParser(unittest.TestCase):
    """Test response parsing functionality."""
    
    def test_extract_tool_call_standard(self):
        """Test standard tool call extraction."""
        response = '''<function_calls>
<invoke name="read_file">
<parameter name="path">/test/file.txt</parameter>
</invoke>
</function_calls>'''
        
        calls = ResponseParser.extract_tool_calls(response)
        
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0].tool_name, "read_file")
        self.assertEqual(calls[0].arguments["path"], "/test/file.txt")
    
    def test_extract_tool_call_multiple_params(self):
        """Test extraction with multiple parameters."""
        response = '''<function_calls>
<invoke name="read_file">
<parameter name="path">/test/file.txt</parameter>
<parameter name="limit">50</parameter>
<parameter name="offset">10</parameter>
</invoke>
</function_calls>'''
        
        calls = ResponseParser.extract_tool_calls(response)
        
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0].arguments["path"], "/test/file.txt")
        self.assertEqual(calls[0].arguments["limit"], 50)
        self.assertEqual(calls[0].arguments["offset"], 10)
    
    def test_extract_tool_call_json_value(self):
        """Test extraction with JSON parameter values."""
        response = '''<function_calls>
<invoke name="execute_python">
<parameter name="code">{"key": "value"}</parameter>
</invoke>
</function_calls>'''
        
        calls = ResponseParser.extract_tool_calls(response)
        
        self.assertEqual(len(calls), 1)
        # JSON should be parsed
        self.assertIsInstance(calls[0].arguments["code"], dict)
    
    def test_is_tool_call(self):
        """Test tool call detection."""
        self.assertTrue(ResponseParser.is_tool_call(
            "<function_calls><invoke name='test'></invoke></function_calls>"
        ))
        self.assertFalse(ResponseParser.is_tool_call(
            "This is just a regular response"
        ))


class TestExecutionEngine(unittest.TestCase):
    """Test execution engine functionality."""
    
    def setUp(self):
        self.engine = ExecutionEngine(enable_security=True)
        self.temp_dir = tempfile.mkdtemp()
    
    def tearDown(self):
        import shutil
        shutil.rmtree(self.temp_dir, ignore_errors=True)
    
    def test_execute_python_simple(self):
        """Test simple Python execution."""
        result = self.engine.execute_python("print('Hello World')")
        
        self.assertTrue(result.success)
        self.assertIn("Hello World", result.stdout)
    
    def test_execute_python_with_result(self):
        """Test Python execution with return value."""
        result = self.engine.execute_python("x = 42\nprint(x)")
        
        self.assertTrue(result.success)
        self.assertIn("42", result.stdout)
    
    def test_execute_shell_simple(self):
        """Test simple shell execution."""
        result = self.engine.execute_shell("echo 'Hello'")
        
        self.assertTrue(result.success)
        self.assertIn("Hello", result.stdout)
    
    def test_read_file(self):
        """Test file reading."""
        test_file = os.path.join(self.temp_dir, "test.txt")
        with open(test_file, 'w') as f:
            f.write("Line 1\nLine 2\nLine 3\n")
        
        result = self.engine.read_file(test_file)
        
        self.assertTrue(result.success)
        self.assertIn("Line 1", result.data["content"])
    
    def test_write_file(self):
        """Test file writing."""
        test_file = os.path.join(self.temp_dir, "write_test.txt")
        
        result = self.engine.write_file(test_file, "Test content")
        
        self.assertTrue(result.success)
        self.assertTrue(os.path.exists(test_file))
        
        with open(test_file, 'r') as f:
            self.assertEqual(f.read(), "Test content")
    
    def test_list_directory(self):
        """Test directory listing."""
        # Create test files
        open(os.path.join(self.temp_dir, "file1.txt"), 'w').close()
        open(os.path.join(self.temp_dir, "file2.txt"), 'w').close()
        os.makedirs(os.path.join(self.temp_dir, "subdir"))
        
        result = self.engine.list_directory(self.temp_dir)
        
        self.assertTrue(result.success)
        self.assertEqual(len(result.data["entries"]), 3)


class TestSecurityValidator(unittest.TestCase):
    """Test security validation."""
    
    def test_validate_shell_command_safe(self):
        """Test validation of safe commands."""
        is_valid, error = SecurityValidator.validate_shell_command("ls -la")
        self.assertTrue(is_valid)
        self.assertEqual(error, "")
    
    def test_validate_shell_command_dangerous(self):
        """Test validation of dangerous commands."""
        is_valid, error = SecurityValidator.validate_shell_command("rm -rf /")
        self.assertFalse(is_valid)
        self.assertIn("dangerous", error.lower())
    
    def test_validate_shell_command_sudo(self):
        """Test blocking of sudo commands."""
        is_valid, error = SecurityValidator.validate_shell_command("sudo apt-get install")
        self.assertFalse(is_valid)
        self.assertIn("sudo", error.lower())
    
    def test_validate_python_code_safe(self):
        """Test validation of safe Python code."""
        is_valid, error = SecurityValidator.validate_python_code("print('Hello')")
        self.assertTrue(is_valid)
    
    def test_validate_python_code_dangerous(self):
        """Test validation of dangerous Python code."""
        is_valid, error = SecurityValidator.validate_python_code(
            "__import__('os').system('rm -rf /')"
        )
        self.assertFalse(is_valid)


class TestResultProcessor(unittest.TestCase):
    """Test result processing functionality."""
    
    def setUp(self):
        self.processor = ResultProcessor()
    
    def test_process_file_content(self):
        """Test processing file content result."""
        result = {
            "success": True,
            "data": {
                "content": "Line 1\nLine 2\nLine 3",
                "lines_read": 3,
                "total_lines": 10,
                "truncated": False
            }
        }
        
        processed = self.processor.process(
            result, result_type="file_content", format=OutputFormat.SUMMARY
        )
        
        self.assertIn("content", processed["content"].lower())
    
    def test_process_directory_listing(self):
        """Test processing directory listing."""
        result = {
            "success": True,
            "data": {
                "path": "/test",
                "entries": [
                    {"name": "file1.txt", "type": "file", "size": 100},
                    {"name": "dir1", "type": "directory", "size": 0}
                ]
            }
        }
        
        processed = self.processor.process(
            result, result_type="directory_listing", format=OutputFormat.SUMMARY
        )
        
        self.assertIn("file1.txt", processed["content"])
    
    def test_truncate_structure(self):
        """Test structure truncation."""
        large_dict = {
            "key_" + str(i): "value_" + str(i)
            for i in range(100)
        }
        
        truncated = self.processor._truncate_structure(large_dict)
        
        self.assertLess(len(truncated), len(large_dict))
        self.assertIn("...", str(truncated))


class TestFallbackStrategies(unittest.TestCase):
    """Test fallback strategies."""
    
    def setUp(self):
        reset_registry()
        register_all_tools()
        self.fallback = FallbackStrategies()
    
    def test_retry_strategy(self):
        """Test retry fallback strategy."""
        failed_call = ToolCall(
            tool_name="read_file",
            arguments={},
            status=CallStatus.ERROR,
            error="Missing required parameter: 'path'"
        )
        
        result = self.fallback._retry_strategy(
            failed_call,
            "Read the main.py file",
            {}
        )
        
        self.assertEqual(result.strategy, FallbackType.RETRY)
        self.assertIn("path", result.message)
    
    def test_alternative_strategy(self):
        """Test alternative tool strategy."""
        failed_call = ToolCall(
            tool_name="unknown_tool",
            arguments={},
            status=CallStatus.ERROR,
            error="Unknown tool: 'unknown_tool'"
        )
        
        result = self.fallback._alternative_strategy(
            failed_call,
            "Search for files",
            {}
        )
        
        self.assertEqual(result.strategy, FallbackType.ALTERNATIVE)
    
    def test_handle_failure_selects_strategy(self):
        """Test automatic strategy selection."""
        failed_call = ToolCall(
            tool_name="read_file",
            arguments={},
            status=CallStatus.ERROR,
            error="Missing required parameter: 'path'"
        )
        
        result = self.fallback.handle_failure(
            failed_call,
            "Read the main.py file"
        )
        
        self.assertIsNotNone(result.strategy)


class TestHelixLLMTools(unittest.TestCase):
    """Test main HelixLLMTools class."""
    
    def setUp(self):
        reset_registry()
    
    def test_initialization(self):
        """Test tool system initialization."""
        def mock_callback(messages):
            return "Test response"
        
        tools = HelixLLMTools()
        system_prompt = tools.initialize(model_callback=mock_callback)
        
        self.assertTrue(tools.initialized)
        self.assertIn("AVAILABLE TOOLS", system_prompt)
        self.assertGreater(len(tools.get_available_tools()), 0)
    
    def test_get_system_prompt(self):
        """Test getting system prompt."""
        def mock_callback(messages):
            return "Test"
        
        tools = create_tools(mock_callback)
        prompt = tools.get_system_prompt()
        
        self.assertIn("AVAILABLE TOOLS", prompt)
        self.assertIn("HOW TO USE TOOLS", prompt)
    
    def test_get_tool_info(self):
        """Test getting tool information."""
        def mock_callback(messages):
            return "Test"
        
        tools = create_tools(mock_callback)
        info = tools.get_tool_info("read_file")
        
        self.assertIsNotNone(info)
        self.assertEqual(info["name"], "read_file")
        self.assertIn("parameters", info)
    
    def test_process_message_with_tool_call(self):
        """Test processing message that triggers tool call."""
        def mock_callback(messages):
            return '''<function_calls>
<invoke name="file_exists">
<parameter name="path">/tmp</parameter>
</invoke>
</function_calls>'''
        
        tools = create_tools(mock_callback)
        result = tools.process_message("Check if /tmp exists")
        
        self.assertIn("final_response", result)
        self.assertEqual(len(result["tool_calls"]), 1)
        self.assertEqual(result["tool_calls"][0].tool_name, "file_exists")


class TestPromptTemplate(unittest.TestCase):
    """Test prompt template generation."""
    
    def setUp(self):
        reset_registry()
        register_all_tools()
        self.registry = get_registry()
    
    def test_build_system_prompt(self):
        """Test system prompt generation."""
        prompt = PromptTemplate.build_system_prompt(self.registry)
        
        self.assertIn("AVAILABLE TOOLS", prompt)
        self.assertIn("HOW TO USE TOOLS", prompt)
        self.assertIn("RULES", prompt)
        self.assertIn("EXAMPLES", prompt)
    
    def test_build_system_prompt_without_examples(self):
        """Test system prompt without examples."""
        prompt = PromptTemplate.build_system_prompt(
            self.registry,
            include_examples=False
        )
        
        self.assertIn("AVAILABLE TOOLS", prompt)
        self.assertNotIn("Example 1", prompt)


class TestRetryManager(unittest.TestCase):
    """Test retry manager functionality."""
    
    def setUp(self):
        self.manager = RetryManager(max_retries=3)
    
    def test_should_retry(self):
        """Test retry decision logic."""
        self.assertTrue(self.manager.should_retry("Timeout", 0))
        self.assertFalse(self.manager.should_retry("Timeout", 3))
        self.assertFalse(self.manager.should_retry("Permission denied", 0))
    
    def test_get_delay(self):
        """Test delay calculation."""
        delay1 = self.manager.get_delay(0)
        delay2 = self.manager.get_delay(1)
        
        self.assertGreater(delay2, delay1)  # Exponential backoff


class TestConfirmationManager(unittest.TestCase):
    """Test confirmation manager."""
    
    def setUp(self):
        self.manager = ConfirmationManager()
    
    def test_requires_confirmation(self):
        """Test confirmation requirement detection."""
        self.assertTrue(self.manager.requires_confirmation("write_file", {}))
        self.assertTrue(self.manager.requires_confirmation("delete", {}))
        self.assertFalse(self.manager.requires_confirmation("read_file", {}))
    
    def test_format_confirmation_prompt(self):
        """Test confirmation prompt formatting."""
        prompt = self.manager.format_confirmation_prompt(
            "write_file",
            {"path": "/test/file.txt"}
        )
        
        self.assertIn("CONFIRMATION REQUIRED", prompt)
        self.assertIn("write_file", prompt)
        self.assertIn("/test/file.txt", prompt)


class IntegrationTests(unittest.TestCase):
    """Integration tests for the complete system."""
    
    def setUp(self):
        reset_registry()
        self.temp_dir = tempfile.mkdtemp()
    
    def tearDown(self):
        import shutil
        shutil.rmtree(self.temp_dir, ignore_errors=True)
    
    def test_end_to_end_file_read(self):
        """Test end-to-end file reading workflow."""
        # Create test file
        test_file = os.path.join(self.temp_dir, "test.py")
        with open(test_file, 'w') as f:
            f.write("print('Hello World')\n")
        
        # Mock model that returns proper tool call
        def mock_model(messages):
            return f'''<function_calls>
<invoke name="read_file">
<parameter name="path">{test_file}</parameter>
</invoke>
</function_calls>'''
        
        tools = create_tools(mock_model, working_dir=self.temp_dir)
        result = tools.process_message("Read the test file")
        
        self.assertEqual(len(result["tool_calls"]), 1)
        self.assertEqual(result["tool_calls"][0].tool_name, "read_file")
        self.assertTrue(result["tool_calls"][0].result["success"])
    
    def test_end_to_end_python_execution(self):
        """Test end-to-end Python execution workflow."""
        def mock_model(messages):
            return '''<function_calls>
<invoke name="execute_python">
<parameter name="code">print(2 + 2)</parameter>
</invoke>
</function_calls>'''
        
        tools = create_tools(mock_model, working_dir=self.temp_dir)
        result = tools.process_message("Calculate 2 + 2")
        
        self.assertEqual(len(result["tool_calls"]), 1)
        self.assertEqual(result["tool_calls"][0].tool_name, "execute_python")
        self.assertIn("4", result["tool_calls"][0].result.get("stdout", ""))


def run_tests():
    """Run all tests."""
    # Create test suite
    loader = unittest.TestLoader()
    suite = unittest.TestSuite()
    
    # Add all test classes
    suite.addTests(loader.loadTestsFromTestCase(TestToolRegistry))
    suite.addTests(loader.loadTestsFromTestCase(TestResponseParser))
    suite.addTests(loader.loadTestsFromTestCase(TestExecutionEngine))
    suite.addTests(loader.loadTestsFromTestCase(TestSecurityValidator))
    suite.addTests(loader.loadTestsFromTestCase(TestResultProcessor))
    suite.addTests(loader.loadTestsFromTestCase(TestFallbackStrategies))
    suite.addTests(loader.loadTestsFromTestCase(TestHelixLLMTools))
    suite.addTests(loader.loadTestsFromTestCase(TestPromptTemplate))
    suite.addTests(loader.loadTestsFromTestCase(TestRetryManager))
    suite.addTests(loader.loadTestsFromTestCase(TestConfirmationManager))
    suite.addTests(loader.loadTestsFromTestCase(IntegrationTests))
    
    # Run tests
    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)
    
    return result.wasSuccessful()


if __name__ == "__main__":
    success = run_tests()
    sys.exit(0 if success else 1)
