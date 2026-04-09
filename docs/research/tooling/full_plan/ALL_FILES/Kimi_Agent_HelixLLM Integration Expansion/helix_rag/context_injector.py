"""
HelixLLM RAG Pipeline - Context Injector
========================================
Prompt template management and context injection for RAG.
Optimized for coding tasks with HelixLLM 1.5B model.
"""

import re
from dataclasses import dataclass, field
from typing import List, Dict, Optional, Any, Callable
from enum import Enum
import logging

logger = logging.getLogger(__name__)


class PromptTemplateType(Enum):
    """Types of prompt templates."""
    CODE_ANALYSIS = "code_analysis"
    CODE_GENERATION = "code_generation"
    DEBUGGING = "debugging"
    DOCUMENTATION = "documentation"
    GENERAL = "general"


@dataclass
class PromptTemplate:
    """A prompt template with configurable sections."""
    name: str
    template: str
    template_type: PromptTemplateType
    description: str = ""
    
    # Token budgets
    max_context_tokens: int = 2048
    max_response_tokens: int = 1024
    system_prompt_tokens: int = 200
    
    # Citation settings
    enable_citations: bool = True
    citation_format: str = "[{index}]"


@dataclass
class InjectedPrompt:
    """Final prompt with injected context."""
    system_prompt: str
    user_prompt: str
    context: str
    full_prompt: str
    citations: List[Dict[str, Any]]
    token_estimate: int


class PromptTemplateLibrary:
    """Library of optimized prompt templates for coding tasks."""
    
    TEMPLATES = {
        PromptTemplateType.CODE_ANALYSIS: PromptTemplate(
            name="code_analysis",
            template_type=PromptTemplateType.CODE_ANALYSIS,
            description="Analyze and explain code",
            max_context_tokens=2048,
            template="""You are a code analysis assistant. Analyze the provided code context and answer the user's question.

## Guidelines:
- Provide clear, concise explanations
- Reference specific parts of the code when relevant
- Suggest improvements if applicable
- Use proper code formatting in your response

## Relevant Code Context:
{context}

## User Question:
{query}

## Your Analysis:"""
        ),
        
        PromptTemplateType.CODE_GENERATION: PromptTemplate(
            name="code_generation",
            template_type=PromptTemplateType.CODE_GENERATION,
            description="Generate code based on requirements",
            max_context_tokens=2048,
            template="""You are a code generation assistant. Generate code based on the user's requirements, using the provided context as reference.

## Guidelines:
- Write clean, well-documented code
- Follow best practices from the context
- Include comments explaining complex logic
- Provide complete, runnable code

## Reference Context:
{context}

## User Request:
{query}

## Generated Code:
```python
""",
            max_response_tokens=2048
        ),
        
        PromptTemplateType.DEBUGGING: PromptTemplate(
            name="debugging",
            template_type=PromptTemplateType.DEBUGGING,
            description="Help debug code issues",
            max_context_tokens=2048,
            template="""You are a debugging assistant. Help identify and fix issues in the code.

## Guidelines:
- Identify the root cause of the problem
- Provide a clear explanation of the issue
- Suggest a specific fix with code
- Explain how to prevent similar issues

## Code Context:
{context}

## Issue Description:
{query}

## Analysis and Solution:"""
        ),
        
        PromptTemplateType.DOCUMENTATION: PromptTemplate(
            name="documentation",
            template_type=PromptTemplateType.DOCUMENTATION,
            description="Answer questions about documentation",
            max_context_tokens=2048,
            template="""You are a documentation assistant. Answer the user's question based on the provided documentation context.

## Guidelines:
- Be accurate and cite specific sections
- Provide examples when available
- Link related concepts when relevant

## Documentation Context:
{context}

## User Question:
{query}

## Answer:"""
        ),
        
        PromptTemplateType.GENERAL: PromptTemplate(
            name="general",
            template_type=PromptTemplateType.GENERAL,
            description="General RAG query",
            max_context_tokens=2048,
            template="""You are a helpful assistant. Answer the user's question using the provided context.

## Context:
{context}

## Question:
{query}

## Answer:"""
        ),
    }
    
    SYSTEM_PROMPTS = {
        PromptTemplateType.CODE_ANALYSIS: """You are HelixLLM, an expert code analysis assistant. Your task is to analyze code and provide insightful explanations.

Key capabilities:
- Understand code structure and logic
- Identify patterns and anti-patterns
- Explain complex algorithms
- Suggest improvements and optimizations

Always be precise and reference specific code sections in your explanations.""",
        
        PromptTemplateType.CODE_GENERATION: """You are HelixLLM, an expert code generation assistant. Your task is to write high-quality, production-ready code.

Key principles:
- Write clean, readable code
- Follow language best practices
- Include proper error handling
- Add helpful comments
- Ensure code is complete and runnable

Generate code that follows the style and patterns from the provided context.""",
        
        PromptTemplateType.DEBUGGING: """You are HelixLLM, an expert debugging assistant. Your task is to help identify and fix code issues.

Approach:
- Carefully analyze the error description and code
- Identify the root cause
- Provide a clear explanation
- Offer a concrete solution with code
- Suggest preventive measures

Be thorough but concise in your analysis.""",
        
        PromptTemplateType.DOCUMENTATION: """You are HelixLLM, a knowledgeable documentation assistant. Your task is to help users understand technical documentation.

Guidelines:
- Provide accurate information from the context
- Cite specific sections when possible
- Clarify complex concepts
- Connect related topics

Be helpful and informative in your responses.""",
        
        PromptTemplateType.GENERAL: """You are HelixLLM, a helpful AI assistant with access to a knowledge base. Answer questions accurately using the provided context.

If the context doesn't contain relevant information, say so clearly.""",
    }
    
    @classmethod
    def get_template(cls, template_type: PromptTemplateType) -> PromptTemplate:
        """Get a prompt template by type."""
        return cls.TEMPLATES.get(template_type, cls.TEMPLATES[PromptTemplateType.GENERAL])
    
    @classmethod
    def get_system_prompt(cls, template_type: PromptTemplateType) -> str:
        """Get system prompt for template type."""
        return cls.SYSTEM_PROMPTS.get(template_type, cls.SYSTEM_PROMPTS[PromptTemplateType.GENERAL])
    
    @classmethod
    def detect_template_type(cls, query: str) -> PromptTemplateType:
        """Auto-detect the best template type for a query."""
        query_lower = query.lower()
        
        # Debugging patterns
        debug_patterns = [
            'error', 'exception', 'bug', 'fix', 'debug', 'traceback',
            'not working', 'broken', 'fails', 'crash'
        ]
        if any(p in query_lower for p in debug_patterns):
            return PromptTemplateType.DEBUGGING
        
        # Code generation patterns
        gen_patterns = [
            'write', 'create', 'generate', 'implement', 'build',
            'function to', 'class for', 'how do i', 'how to make'
        ]
        if any(p in query_lower for p in gen_patterns):
            return PromptTemplateType.CODE_GENERATION
        
        # Documentation patterns
        doc_patterns = [
            'what is', 'how does', 'explain', 'documentation',
            'api', 'reference', 'usage', 'parameter'
        ]
        if any(p in query_lower for p in doc_patterns):
            return PromptTemplateType.DOCUMENTATION
        
        # Code analysis patterns
        analysis_patterns = [
            'analyze', 'understand', 'review', 'optimize',
            'improve', 'refactor', 'pattern'
        ]
        if any(p in query_lower for p in analysis_patterns):
            return PromptTemplateType.CODE_ANALYSIS
        
        return PromptTemplateType.GENERAL


class TokenBudgetManager:
    """Manage token budgets for context injection."""
    
    # Approximate tokens per character for different content types
    TOKENS_PER_CHAR = {
        'code': 0.3,      # Code has more tokens per char
        'text': 0.25,     # Average text
        'markdown': 0.28, # Markdown with formatting
    }
    
    def __init__(self, max_tokens: int = 4096):
        self.max_tokens = max_tokens
        
    def estimate_tokens(self, text: str, content_type: str = 'text') -> int:
        """Estimate token count for text."""
        multiplier = self.TOKENS_PER_CHAR.get(content_type, 0.25)
        return int(len(text) * multiplier)
    
    def allocate_budget(
        self,
        system_prompt: str,
        query: str,
        available_context_tokens: int,
        response_tokens: int = 1024
    ) -> Dict[str, int]:
        """
        Allocate token budget across prompt components.
        
        Returns:
            Dictionary with token allocations
        """
        system_tokens = self.estimate_tokens(system_prompt, 'text')
        query_tokens = self.estimate_tokens(query, 'text')
        
        # Reserve tokens for response
        reserved = system_tokens + query_tokens + response_tokens + 50  # Buffer
        
        # Available for context
        context_tokens = min(available_context_tokens, self.max_tokens - reserved)
        
        return {
            'system': system_tokens,
            'query': query_tokens,
            'context': max(0, context_tokens),
            'response': response_tokens,
            'buffer': 50,
            'total': system_tokens + query_tokens + max(0, context_tokens) + response_tokens + 50,
            'max': self.max_tokens,
        }


class ContextInjector:
    """
    Inject retrieved context into prompts.
    
    Features:
    - Template-based prompt generation
    - Token budget management
    - Citation tracking
    - Multi-context formatting
    """
    
    def __init__(
        self,
        max_tokens: int = 4096,
        default_template: Optional[PromptTemplateType] = None
    ):
        self.token_manager = TokenBudgetManager(max_tokens)
        self.default_template = default_template or PromptTemplateType.GENERAL
        self.template_library = PromptTemplateLibrary()
        
    def inject(
        self,
        query: str,
        contexts: List[Any],
        template_type: Optional[PromptTemplateType] = None,
        custom_template: Optional[str] = None,
        response_tokens: int = 1024
    ) -> InjectedPrompt:
        """
        Inject context into a prompt template.
        
        Args:
            query: User query
            contexts: Retrieved contexts to inject
            template_type: Type of template to use
            custom_template: Optional custom template string
            response_tokens: Tokens to reserve for response
            
        Returns:
            InjectedPrompt with formatted prompt and metadata
        """
        # Detect template type if not specified
        if template_type is None:
            template_type = self.template_library.detect_template_type(query)
        
        # Get template
        if custom_template:
            template = PromptTemplate(
                name="custom",
                template=custom_template,
                template_type=template_type
            )
        else:
            template = self.template_library.get_template(template_type)
        
        # Get system prompt
        system_prompt = self.template_library.get_system_prompt(template_type)
        
        # Calculate token budget
        budget = self.token_manager.allocate_budget(
            system_prompt=system_prompt,
            query=query,
            available_context_tokens=template.max_context_tokens,
            response_tokens=response_tokens
        )
        
        # Format context within budget
        formatted_context, citations = self._format_contexts(
            contexts, 
            budget['context'],
            template.enable_citations
        )
        
        # Build user prompt
        user_prompt = template.template.format(
            context=formatted_context,
            query=query
        )
        
        # Build full prompt (for models that don't use system/user separation)
        full_prompt = f"{system_prompt}\n\n{user_prompt}"
        
        # Estimate total tokens
        token_estimate = (
            self.token_manager.estimate_tokens(system_prompt) +
            self.token_manager.estimate_tokens(user_prompt)
        )
        
        return InjectedPrompt(
            system_prompt=system_prompt,
            user_prompt=user_prompt,
            context=formatted_context,
            full_prompt=full_prompt,
            citations=citations,
            token_estimate=token_estimate
        )
    
    def _format_contexts(
        self,
        contexts: List[Any],
        max_tokens: int,
        enable_citations: bool
    ) -> tuple:
        """
        Format contexts within token budget.
        
        Returns:
            Tuple of (formatted_context, citations)
        """
        formatted_parts = []
        citations = []
        current_tokens = 0
        
        for i, ctx in enumerate(contexts):
            # Format context entry
            citation_num = i + 1
            
            # Build context entry
            entry_parts = []
            
            if enable_citations:
                entry_parts.append(f"[{citation_num}] ")
            
            # Add source info
            source_info = f"Source: {ctx.source_file}"
            if ctx.start_line:
                source_info += f" (lines {ctx.start_line}-{ctx.end_line})"
            entry_parts.append(f"{source_info}\n")
            
            # Add content
            entry_parts.append(ctx.content)
            
            entry_text = "".join(entry_parts)
            entry_tokens = self.token_manager.estimate_tokens(entry_text, 'code')
            
            # Check budget
            if current_tokens + entry_tokens > max_tokens:
                break
            
            formatted_parts.append(entry_text)
            current_tokens += entry_tokens
            
            # Track citation
            citations.append({
                'index': citation_num,
                'chunk_id': ctx.chunk_id,
                'source_file': ctx.source_file,
                'start_line': ctx.start_line,
                'end_line': ctx.end_line,
                'score': getattr(ctx, 'score', None),
            })
        
        formatted_context = "\n\n---\n\n".join(formatted_parts)
        
        return formatted_context, citations
    
    def create_citation_string(self, citations: List[Dict]) -> str:
        """Create a formatted citation list."""
        if not citations:
            return ""
        
        lines = ["\n\n## Sources:"]
        
        for cit in citations:
            line = f"[{cit['index']}] {cit['source_file']}"
            if cit.get('start_line'):
                line += f" (lines {cit['start_line']}-{cit['end_line']})"
            lines.append(line)
        
        return "\n".join(lines)


class RAGPromptBuilder:
    """Builder for creating complex RAG prompts."""
    
    def __init__(self, injector: ContextInjector):
        self.injector = injector
        self.contexts = []
        self.query = ""
        self.template_type = PromptTemplateType.GENERAL
        
    def with_query(self, query: str) -> 'RAGPromptBuilder':
        """Set the query."""
        self.query = query
        return self
    
    def with_contexts(self, contexts: List[Any]) -> 'RAGPromptBuilder':
        """Set the contexts."""
        self.contexts = contexts
        return self
    
    def with_template(self, template_type: PromptTemplateType) -> 'RAGPromptBuilder':
        """Set the template type."""
        self.template_type = template_type
        return self
    
    def build(self, response_tokens: int = 1024) -> InjectedPrompt:
        """Build the final prompt."""
        return self.injector.inject(
            query=self.query,
            contexts=self.contexts,
            template_type=self.template_type,
            response_tokens=response_tokens
        )


# Example usage
if __name__ == "__main__":
    from retrieval_engine import RetrievedContext
    
    # Create injector
    injector = ContextInjector(max_tokens=4096)
    
    # Sample contexts
    contexts = [
        RetrievedContext(
            chunk_id="abc123",
            content="def binary_search(arr, target):\n    left, right = 0, len(arr) - 1\n    while left <= right:\n        mid = (left + right) // 2\n        if arr[mid] == target:\n            return mid\n        elif arr[mid] < target:\n            left = mid + 1\n        else:\n            right = mid - 1\n    return -1",
            source_file="/path/to/algorithms.py",
            score=0.92,
            rank=0,
            start_line=10,
            end_line=25,
            language="python"
        ),
        RetrievedContext(
            chunk_id="def456",
            content="Binary search is an efficient algorithm for finding an item from a sorted list of items. It works by repeatedly dividing in half the portion of the list that could contain the item.",
            source_file="/path/to/docs.md",
            score=0.85,
            rank=1,
            start_line=45,
            end_line=50,
        )
    ]
    
    # Test query
    query = "How do I implement binary search in Python?"
    
    # Detect template type
    template_type = PromptTemplateLibrary.detect_template_type(query)
    print(f"Detected template type: {template_type.value}")
    
    # Inject context
    injected = injector.inject(
        query=query,
        contexts=contexts,
        template_type=template_type,
        response_tokens=1024
    )
    
    print(f"\nToken estimate: {injected.token_estimate}")
    print(f"Number of citations: {len(injected.citations)}")
    
    print("\n" + "="*60)
    print("SYSTEM PROMPT:")
    print("="*60)
    print(injected.system_prompt[:500] + "...")
    
    print("\n" + "="*60)
    print("USER PROMPT:")
    print("="*60)
    print(injected.user_prompt[:800] + "...")
    
    print("\n" + "="*60)
    print("CITATIONS:")
    print("="*60)
    for cit in injected.citations:
        print(f"[{cit['index']}] {cit['source_file']}:{cit.get('start_line', 'N/A')}")
