"""
HelixLLM RAG Pipeline - Document Processing
===========================================
Code-aware document processing with intelligent chunking strategies.
Supports: .txt, .md, .py, .js, .ts, .json, .yaml, .pdf
"""

import os
import re
import json
import hashlib
from pathlib import Path
from dataclasses import dataclass, field
from typing import List, Dict, Optional, Callable, Union, Any, Tuple
from enum import Enum
import logging

logger = logging.getLogger(__name__)


class FileType(Enum):
    """Supported file types."""
    TEXT = "txt"
    MARKDOWN = "md"
    PYTHON = "py"
    JAVASCRIPT = "js"
    TYPESCRIPT = "ts"
    JSON = "json"
    YAML = "yaml"
    PDF = "pdf"
    UNKNOWN = "unknown"


@dataclass
class ChunkConfig:
    """Configuration for document chunking."""
    # General chunking
    chunk_size: int = 512  # Target chunk size in tokens (approx chars)
    chunk_overlap: int = 128  # Overlap between chunks
    min_chunk_size: int = 64  # Minimum chunk size
    
    # Code-specific
    preserve_functions: bool = True  # Keep functions intact
    preserve_classes: bool = True  # Keep classes intact
    max_function_lines: int = 100  # Split large functions
    
    # Markdown-specific
    preserve_headers: bool = True  # Keep header context
    header_inheritance: bool = True  # Include headers in chunks
    
    # Metadata
    extract_line_numbers: bool = True
    extract_imports: bool = True
    extract_docstrings: bool = True


@dataclass
class DocumentChunk:
    """A single document chunk with metadata."""
    content: str
    source_file: str
    chunk_id: str
    file_type: FileType
    
    # Position info
    start_line: int = 0
    end_line: int = 0
    start_char: int = 0
    end_char: int = 0
    
    # Context
    headers: List[str] = field(default_factory=list)
    parent_function: Optional[str] = None
    parent_class: Optional[str] = None
    
    # Code metadata
    language: Optional[str] = None
    imports: List[str] = field(default_factory=list)
    docstring: Optional[str] = None
    
    # Chunking metadata
    chunk_index: int = 0
    total_chunks: int = 0
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert chunk to dictionary."""
        return {
            "content": self.content,
            "source_file": self.source_file,
            "chunk_id": self.chunk_id,
            "file_type": self.file_type.value,
            "start_line": self.start_line,
            "end_line": self.end_line,
            "headers": self.headers,
            "parent_function": self.parent_function,
            "parent_class": self.parent_class,
            "language": self.language,
            "chunk_index": self.chunk_index,
            "total_chunks": self.total_chunks,
        }
    
    def get_embedding_text(self) -> str:
        """Get text optimized for embedding."""
        parts = []
        
        # Add headers for context
        if self.headers:
            parts.append(" | ".join(self.headers))
        
        # Add parent context for code
        if self.parent_class:
            parts.append(f"Class: {self.parent_class}")
        if self.parent_function:
            parts.append(f"Function: {self.parent_function}")
        
        # Add main content
        parts.append(self.content)
        
        return "\n".join(parts)


class CodeParser:
    """Parse code files to extract structure."""
    
    # Language patterns
    PATTERNS = {
        FileType.PYTHON: {
            'function': r'^(?:async\s+)?def\s+(\w+)\s*\(',
            'class': r'^class\s+(\w+)(?:\s*\(|:)',
            'import': r'^(?:from\s+(\S+)\s+)?import\s+(.+)$',
            'docstring': r'^[\'"]{3}(.*?)[\'"]{3}',
            'comment': r'#.*$',
        },
        FileType.JAVASCRIPT: {
            'function': r'^(?:async\s+)?(?:function\s+(\w+)|(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?\(',
            'class': r'^class\s+(\w+)',
            'import': r'^import\s+(.+)\s+from\s+[\'"](.+)[\'"]',
            'docstring': r'/\*\*(.*?)\*/',
            'comment': r'//.*$',
        },
        FileType.TYPESCRIPT: {
            'function': r'^(?:async\s+)?(?:function\s+(\w+)|(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?\(',
            'class': r'^(?:export\s+)?class\s+(\w+)',
            'interface': r'^interface\s+(\w+)',
            'import': r'^import\s+(.+)\s+from\s+[\'"](.+)[\'"]',
            'docstring': r'/\*\*(.*?)\*/',
            'comment': r'//.*$',
        },
    }
    
    @classmethod
    def get_file_type(cls, file_path: str) -> FileType:
        """Determine file type from extension."""
        ext = Path(file_path).suffix.lower()
        type_map = {
            '.txt': FileType.TEXT,
            '.md': FileType.MARKDOWN,
            '.py': FileType.PYTHON,
            '.js': FileType.JAVASCRIPT,
            '.ts': FileType.TYPESCRIPT,
            '.json': FileType.JSON,
            '.yaml': FileType.YAML,
            '.yml': FileType.YAML,
            '.pdf': FileType.PDF,
        }
        return type_map.get(ext, FileType.UNKNOWN)
    
    @classmethod
    def parse_code_structure(
        cls, 
        content: str, 
        file_type: FileType
    ) -> Dict[str, Any]:
        """Parse code to extract functions, classes, and structure."""
        if file_type not in cls.PATTERNS:
            return {"blocks": []}
        
        patterns = cls.PATTERNS[file_type]
        lines = content.split('\n')
        
        blocks = []
        current_class = None
        current_function = None
        block_start = 0
        
        for i, line in enumerate(lines):
            # Check for class
            class_match = re.match(patterns['class'], line)
            if class_match:
                # Save previous block
                if current_function and block_start < i:
                    blocks.append({
                        'type': 'function',
                        'name': current_function,
                        'class': current_class,
                        'start': block_start,
                        'end': i - 1,
                    })
                
                current_class = class_match.group(1)
                current_function = None
                block_start = i
            
            # Check for function
            func_match = re.match(patterns['function'], line)
            if func_match:
                # Save previous block
                if current_function and block_start < i:
                    blocks.append({
                        'type': 'function',
                        'name': current_function,
                        'class': current_class,
                        'start': block_start,
                        'end': i - 1,
                    })
                
                current_function = func_match.group(1) or func_match.group(2)
                block_start = i
        
        # Save final block
        if current_function and block_start < len(lines):
            blocks.append({
                'type': 'function',
                'name': current_function,
                'class': current_class,
                'start': block_start,
                'end': len(lines) - 1,
            })
        
        return {"blocks": blocks}
    
    @classmethod
    def extract_imports(cls, content: str, file_type: FileType) -> List[str]:
        """Extract import statements from code."""
        if file_type not in cls.PATTERNS:
            return []
        
        pattern = cls.PATTERNS[file_type].get('import')
        if not pattern:
            return []
        
        imports = []
        for line in content.split('\n'):
            match = re.match(pattern, line.strip())
            if match:
                imports.append(line.strip())
        
        return imports


class DocumentProcessor:
    """
    Process documents with code-aware chunking.
    """
    
    def __init__(self, config: Optional[ChunkConfig] = None):
        self.config = config or ChunkConfig()
        self.code_parser = CodeParser()
    
    def process_file(self, file_path: str) -> List[DocumentChunk]:
        """
        Process a single file into chunks.
        
        Args:
            file_path: Path to the file
            
        Returns:
            List of document chunks
        """
        file_path = Path(file_path)
        
        if not file_path.exists():
            logger.error(f"File not found: {file_path}")
            return []
        
        file_type = self.code_parser.get_file_type(str(file_path))
        
        # Read content based on file type
        try:
            if file_type == FileType.PDF:
                content = self._read_pdf(str(file_path))
            else:
                content = file_path.read_text(encoding='utf-8', errors='ignore')
        except Exception as e:
            logger.error(f"Error reading {file_path}: {e}")
            return []
        
        # Process based on file type
        if file_type in (FileType.PYTHON, FileType.JAVASCRIPT, FileType.TYPESCRIPT):
            return self._chunk_code(content, str(file_path), file_type)
        elif file_type == FileType.MARKDOWN:
            return self._chunk_markdown(content, str(file_path))
        elif file_type == FileType.JSON:
            return self._chunk_json(content, str(file_path))
        else:
            return self._chunk_text(content, str(file_path), file_type)
    
    def process_directory(
        self, 
        directory: str, 
        include_patterns: List[str] = None,
        exclude_patterns: List[str] = None
    ) -> List[DocumentChunk]:
        """
        Process all files in a directory.
        
        Args:
            directory: Root directory to process
            include_patterns: Glob patterns to include (e.g., ["*.py", "*.md"])
            exclude_patterns: Glob patterns to exclude (e.g., ["*/venv/*", "*/.git/*"])
            
        Returns:
            List of all document chunks
        """
        directory = Path(directory)
        
        if not directory.exists():
            logger.error(f"Directory not found: {directory}")
            return []
        
        # Default patterns
        include_patterns = include_patterns or ["*.py", "*.js", "*.ts", "*.md", "*.txt", "*.json", "*.yaml", "*.yml"]
        exclude_patterns = exclude_patterns or [
            "*/venv/*", "*/.venv/*", "*/env/*",
            "*/.git/*", "*/__pycache__/*", "*/node_modules/*",
            "*/.pytest_cache/*", "*/build/*", "*/dist/*",
            "*.min.js", "*.min.css", "*/.tox/*"
        ]
        
        all_chunks = []
        
        for pattern in include_patterns:
            for file_path in directory.rglob(pattern):
                # Check exclude patterns
                excluded = any(file_path.match(excl) for excl in exclude_patterns)
                str_path = str(file_path)
                excluded = excluded or any(
                    excl.strip('*').replace('/', '') in str_path 
                    for excl in exclude_patterns
                )
                
                if not excluded:
                    chunks = self.process_file(str(file_path))
                    all_chunks.extend(chunks)
                    logger.info(f"Processed {file_path}: {len(chunks)} chunks")
        
        return all_chunks
    
    def _read_pdf(self, file_path: str) -> str:
        """Read PDF file."""
        try:
            import pypdf
            text = ""
            with open(file_path, 'rb') as f:
                reader = pypdf.PdfReader(f)
                for page in reader.pages:
                    text += page.extract_text() + "\n"
            return text
        except ImportError:
            logger.error("pypdf not installed. Install with: pip install pypdf")
            return ""
    
    def _chunk_code(
        self, 
        content: str, 
        file_path: str, 
        file_type: FileType
    ) -> List[DocumentChunk]:
        """Chunk code files preserving structure."""
        lines = content.split('\n')
        structure = self.code_parser.parse_code_structure(content, file_type)
        imports = self.code_parser.extract_imports(content, file_type)
        
        chunks = []
        chunk_index = 0
        
        # Process by blocks if available
        if structure['blocks'] and self.config.preserve_functions:
            for block in structure['blocks']:
                block_lines = lines[block['start']:block['end'] + 1]
                block_content = '\n'.join(block_lines)
                
                # Split large blocks
                if len(block_lines) > self.config.max_function_lines:
                    sub_chunks = self._split_large_block(
                        block_content, block['start'], file_path, file_type
                    )
                    for sub_chunk in sub_chunks:
                        sub_chunk.parent_function = block['name']
                        sub_chunk.parent_class = block['class']
                        sub_chunk.imports = imports
                        sub_chunk.language = file_type.value
                        sub_chunk.chunk_index = chunk_index
                        chunks.append(sub_chunk)
                        chunk_index += 1
                else:
                    chunk = DocumentChunk(
                        content=block_content,
                        source_file=file_path,
                        chunk_id=self._generate_chunk_id(file_path, block['start']),
                        file_type=file_type,
                        start_line=block['start'] + 1,
                        end_line=block['end'] + 1,
                        parent_function=block['name'],
                        parent_class=block['class'],
                        imports=imports,
                        language=file_type.value,
                        chunk_index=chunk_index,
                    )
                    chunks.append(chunk)
                    chunk_index += 1
        
        # Add any remaining content as chunks
        if not chunks:
            chunks = self._chunk_text(content, file_path, file_type)
        
        # Update total chunks
        for chunk in chunks:
            chunk.total_chunks = len(chunks)
        
        return chunks
    
    def _split_large_block(
        self, 
        content: str, 
        start_line: int,
        file_path: str,
        file_type: FileType
    ) -> List[DocumentChunk]:
        """Split a large code block into smaller chunks."""
        lines = content.split('\n')
        chunks = []
        
        i = 0
        while i < len(lines):
            # Find chunk end
            chunk_end = min(i + self.config.chunk_size // 50, len(lines))  # Approx 50 chars per line
            
            # Extend to end of statement if possible
            while chunk_end < len(lines) and chunk_end > i:
                if lines[chunk_end - 1].strip().endswith((':', ';', '{', '}', ')', ']')):
                    break
                chunk_end -= 1
            
            chunk_lines = lines[i:chunk_end]
            chunk_content = '\n'.join(chunk_lines)
            
            chunk = DocumentChunk(
                content=chunk_content,
                source_file=file_path,
                chunk_id=self._generate_chunk_id(file_path, start_line + i),
                file_type=file_type,
                start_line=start_line + i + 1,
                end_line=start_line + chunk_end,
            )
            chunks.append(chunk)
            
            # Move with overlap
            i = chunk_end - (self.config.chunk_overlap // 50)
        
        return chunks
    
    def _chunk_markdown(self, content: str, file_path: str) -> List[DocumentChunk]:
        """Chunk markdown preserving headers."""
        lines = content.split('\n')
        chunks = []
        
        current_headers = []
        current_content = []
        current_start = 0
        chunk_index = 0
        
        header_pattern = re.compile(r'^(#{1,6})\s+(.+)$')
        
        for i, line in enumerate(lines):
            header_match = header_pattern.match(line)
            
            if header_match:
                # Save previous chunk
                if current_content:
                    chunk_text = '\n'.join(current_content)
                    if len(chunk_text) >= self.config.min_chunk_size:
                        chunk = DocumentChunk(
                            content=chunk_text,
                            source_file=file_path,
                            chunk_id=self._generate_chunk_id(file_path, current_start),
                            file_type=FileType.MARKDOWN,
                            start_line=current_start + 1,
                            end_line=i,
                            headers=current_headers.copy(),
                            language='markdown',
                            chunk_index=chunk_index,
                        )
                        chunks.append(chunk)
                        chunk_index += 1
                
                # Update headers
                level = len(header_match.group(1))
                header_text = header_match.group(2)
                current_headers = current_headers[:level-1] + [header_text]
                
                current_content = [line]
                current_start = i
            else:
                current_content.append(line)
                
                # Check if chunk is large enough
                content_text = '\n'.join(current_content)
                if len(content_text) >= self.config.chunk_size:
                    chunk = DocumentChunk(
                        content=content_text,
                        source_file=file_path,
                        chunk_id=self._generate_chunk_id(file_path, current_start),
                        file_type=FileType.MARKDOWN,
                        start_line=current_start + 1,
                        end_line=i + 1,
                        headers=current_headers.copy(),
                        language='markdown',
                        chunk_index=chunk_index,
                    )
                    chunks.append(chunk)
                    chunk_index += 1
                    
                    # Start new chunk with overlap
                    overlap_lines = current_content[-self.config.chunk_overlap // 50:]
                    current_content = current_headers + [''] + overlap_lines
                    current_start = i - len(overlap_lines) + 1
        
        # Save final chunk
        if current_content:
            chunk_text = '\n'.join(current_content)
            if len(chunk_text) >= self.config.min_chunk_size:
                chunk = DocumentChunk(
                    content=chunk_text,
                    source_file=file_path,
                    chunk_id=self._generate_chunk_id(file_path, current_start),
                    file_type=FileType.MARKDOWN,
                    start_line=current_start + 1,
                    end_line=len(lines),
                    headers=current_headers.copy(),
                    language='markdown',
                    chunk_index=chunk_index,
                )
                chunks.append(chunk)
                chunk_index += 1
        
        # Update total chunks
        for chunk in chunks:
            chunk.total_chunks = len(chunks)
        
        return chunks
    
    def _chunk_json(self, content: str, file_path: str) -> List[DocumentChunk]:
        """Chunk JSON files by top-level keys."""
        try:
            data = json.loads(content)
            
            if isinstance(data, dict):
                chunks = []
                chunk_index = 0
                
                for key, value in data.items():
                    chunk_content = json.dumps({key: value}, indent=2)
                    
                    chunk = DocumentChunk(
                        content=chunk_content,
                        source_file=file_path,
                        chunk_id=self._generate_chunk_id(file_path, chunk_index),
                        file_type=FileType.JSON,
                        start_line=0,
                        end_line=0,
                        headers=[key],
                        language='json',
                        chunk_index=chunk_index,
                    )
                    chunks.append(chunk)
                    chunk_index += 1
                
                # Update total chunks
                for chunk in chunks:
                    chunk.total_chunks = len(chunks)
                
                return chunks
            else:
                # Fallback to text chunking
                return self._chunk_text(content, file_path, FileType.JSON)
                
        except json.JSONDecodeError:
            return self._chunk_text(content, file_path, FileType.JSON)
    
    def _chunk_text(
        self, 
        content: str, 
        file_path: str, 
        file_type: FileType
    ) -> List[DocumentChunk]:
        """Generic text chunking with overlap."""
        chunks = []
        chunk_index = 0
        
        start = 0
        content_len = len(content)
        
        while start < content_len:
            end = min(start + self.config.chunk_size, content_len)
            
            # Try to break at newline or sentence
            if end < content_len:
                # Look for newline
                newline_pos = content.rfind('\n', start, end)
                if newline_pos > start + self.config.min_chunk_size:
                    end = newline_pos + 1
                else:
                    # Look for sentence end
                    for delim in ['. ', '! ', '? ', '; ']:
                        pos = content.rfind(delim, start, end)
                        if pos > start + self.config.min_chunk_size:
                            end = pos + 2
                            break
            
            chunk_content = content[start:end].strip()
            
            if len(chunk_content) >= self.config.min_chunk_size:
                # Calculate line numbers
                start_line = content[:start].count('\n') + 1
                end_line = content[:end].count('\n') + 1
                
                chunk = DocumentChunk(
                    content=chunk_content,
                    source_file=file_path,
                    chunk_id=self._generate_chunk_id(file_path, start),
                    file_type=file_type,
                    start_line=start_line,
                    end_line=end_line,
                    language=file_type.value if file_type != FileType.UNKNOWN else None,
                    chunk_index=chunk_index,
                )
                chunks.append(chunk)
                chunk_index += 1
            
            # Move with overlap
            start = end - self.config.chunk_overlap
            if start >= end:
                start = end
        
        # Update total chunks
        for chunk in chunks:
            chunk.total_chunks = len(chunks)
        
        return chunks
    
    def _generate_chunk_id(self, file_path: str, position: int) -> str:
        """Generate unique chunk ID."""
        unique_string = f"{file_path}:{position}"
        return hashlib.md5(unique_string.encode()).hexdigest()[:16]


# Example usage
if __name__ == "__main__":
    # Create processor
    config = ChunkConfig(
        chunk_size=512,
        chunk_overlap=128,
        preserve_functions=True,
        preserve_classes=True
    )
    processor = DocumentProcessor(config)
    
    # Test with sample code
    sample_code = '''
class DataProcessor:
    """Process data efficiently."""
    
    def __init__(self, config):
        self.config = config
    
    def process(self, data):
        """Process the data."""
        result = []
        for item in data:
            result.append(self.transform(item))
        return result
    
    def transform(self, item):
        """Transform a single item."""
        return item * 2

def main():
    processor = DataProcessor({})
    data = [1, 2, 3, 4, 5]
    result = processor.process(data)
    print(result)
'''
    
    # Write test file
    test_file = "/tmp/test_sample.py"
    with open(test_file, 'w') as f:
        f.write(sample_code)
    
    # Process file
    chunks = processor.process_file(test_file)
    
    print(f"Generated {len(chunks)} chunks:")
    for chunk in chunks:
        print(f"\n--- Chunk {chunk.chunk_index + 1}/{chunk.total_chunks} ---")
        print(f"File: {chunk.source_file}")
        print(f"Lines: {chunk.start_line}-{chunk.end_line}")
        print(f"Function: {chunk.parent_function}")
        print(f"Class: {chunk.parent_class}")
        print(f"Content preview: {chunk.content[:100]}...")
