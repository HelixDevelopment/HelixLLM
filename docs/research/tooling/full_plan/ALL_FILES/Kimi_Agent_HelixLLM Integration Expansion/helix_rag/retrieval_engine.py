"""
HelixLLM RAG Pipeline - Retrieval Engine
========================================
Advanced retrieval with hybrid search, re-ranking, and context management.
"""

import re
import numpy as np
from dataclasses import dataclass, field
from typing import List, Dict, Optional, Any, Callable, Tuple
from enum import Enum
import logging

logger = logging.getLogger(__name__)


class ReRankerType(Enum):
    """Types of re-ranking strategies."""
    NONE = "none"
    CROSS_ENCODER = "cross_encoder"
    DIVERSITY = "diversity"
    RECENCY = "recency"
    HYBRID = "hybrid"


@dataclass
class RetrievalConfig:
    """Configuration for retrieval engine."""
    # Search parameters
    top_k: int = 10  # Initial retrieval count
    final_k: int = 5  # Final results after re-ranking
    
    # Hybrid search weights
    semantic_weight: float = 0.7
    keyword_weight: float = 0.3
    
    # Re-ranking
    reranker_type: ReRankerType = ReRankerType.HYBRID
    cross_encoder_model: str = "cross-encoder/ms-marco-MiniLM-L-6-v2"
    
    # Context management
    max_context_tokens: int = 2048  # Token budget for context
    tokens_per_char: float = 0.25  # Approximate tokens per character
    context_overlap: int = 100  # Overlap between context chunks
    
    # Filtering
    min_score_threshold: float = 0.3
    deduplicate_results: bool = True
    diversity_lambda: float = 0.5  # MMR diversity parameter


@dataclass
class RetrievedContext:
    """A retrieved context chunk with full metadata."""
    chunk_id: str
    content: str
    source_file: str
    score: float
    rank: int
    
    # Metadata
    file_type: Optional[str] = None
    start_line: int = 0
    end_line: int = 0
    language: Optional[str] = None
    headers: List[str] = field(default_factory=list)
    parent_function: Optional[str] = None
    parent_class: Optional[str] = None
    
    # Context management
    token_count: int = 0
    context_position: int = 0  # Position in final context
    
    def to_prompt_format(self, include_metadata: bool = True) -> str:
        """Format for LLM prompt."""
        parts = []
        
        if include_metadata:
            parts.append(f"[Source: {self.source_file}")
            if self.start_line:
                parts.append(f":{self.start_line}-{self.end_line}")
            parts.append("]")
            
            if self.parent_class:
                parts.append(f"[Class: {self.parent_class}]")
            if self.parent_function:
                parts.append(f"[Function: {self.parent_function}]")
            
            parts.append("\n")
        
        parts.append(self.content)
        
        return "".join(parts)


class CrossEncoderReRanker:
    """Cross-encoder re-ranking for precise relevance scoring."""
    
    def __init__(self, model_name: str = "cross-encoder/ms-marco-MiniLM-L-6-v2"):
        self.model_name = model_name
        self.model = None
        
    def load(self) -> bool:
        """Load the cross-encoder model."""
        try:
            from sentence_transformers import CrossEncoder
            
            logger.info(f"Loading cross-encoder: {self.model_name}")
            self.model = CrossEncoder(self.model_name)
            return True
        except ImportError:
            logger.warning("sentence-transformers not installed. Re-ranking disabled.")
            return False
        except Exception as e:
            logger.error(f"Failed to load cross-encoder: {e}")
            return False
    
    def rerank(
        self, 
        query: str, 
        results: List[Any], 
        top_k: int = 5
    ) -> List[Tuple[Any, float]]:
        """
        Re-rank results using cross-encoder.
        
        Args:
            query: Original query
            results: Search results to re-rank
            top_k: Number of top results to return
            
        Returns:
            List of (result, score) tuples
        """
        if self.model is None:
            if not self.load():
                return [(r, r.score) for r in results[:top_k]]
        
        if not results:
            return []
        
        # Prepare pairs for cross-encoder
        pairs = [(query, r.content) for r in results]
        
        # Get scores
        scores = self.model.predict(pairs)
        
        # Combine with results
        scored_results = list(zip(results, scores))
        
        # Sort by cross-encoder score
        scored_results.sort(key=lambda x: x[1], reverse=True)
        
        return scored_results[:top_k]


class MMRReRanker:
    """Maximal Marginal Relevance re-ranking for diversity."""
    
    def __init__(self, lambda_param: float = 0.5):
        self.lambda_param = lambda_param
        
    def rerank(
        self,
        query_embedding: np.ndarray,
        results: List[Any],
        embeddings: np.ndarray,
        top_k: int = 5
    ) -> List[Tuple[Any, float]]:
        """
        Re-rank using MMR for diversity.
        
        MMR = λ * Sim(query, doc) - (1-λ) * max(Sim(doc, selected))
        
        Args:
            query_embedding: Query embedding
            results: Search results
            embeddings: Document embeddings
            top_k: Number of results to return
            
        Returns:
            List of (result, mmr_score) tuples
        """
        if len(results) <= top_k:
            return [(r, r.score) for r in results]
        
        # Normalize embeddings
        query_emb = query_embedding / np.linalg.norm(query_embedding)
        doc_embs = embeddings / np.linalg.norm(embeddings, axis=1, keepdims=True)
        
        # Compute query similarities
        query_sims = np.dot(doc_embs, query_emb)
        
        selected = []
        selected_indices = []
        remaining = list(range(len(results)))
        
        while len(selected) < top_k and remaining:
            mmr_scores = []
            
            for idx in remaining:
                # Relevance to query
                relevance = query_sims[idx]
                
                # Diversity penalty
                if selected_indices:
                    sim_to_selected = [np.dot(doc_embs[idx], doc_embs[s]) for s in selected_indices]
                    max_sim = max(sim_to_selected)
                else:
                    max_sim = 0
                
                # MMR score
                mmr_score = self.lambda_param * relevance - (1 - self.lambda_param) * max_sim
                mmr_scores.append((idx, mmr_score))
            
            # Select best
            best_idx, best_score = max(mmr_scores, key=lambda x: x[1])
            selected.append((results[best_idx], best_score))
            selected_indices.append(best_idx)
            remaining.remove(best_idx)
        
        return selected


class RetrievalEngine:
    """
    Advanced retrieval engine with hybrid search and re-ranking.
    """
    
    def __init__(
        self, 
        vector_store: Any,
        embedder: Any,
        config: Optional[RetrievalConfig] = None
    ):
        self.vector_store = vector_store
        self.embedder = embedder
        self.config = config or RetrievalConfig()
        
        # Re-rankers
        self.cross_encoder = CrossEncoderReRanker(self.config.cross_encoder_model)
        self.mmr_reranker = MMRReRanker(self.config.diversity_lambda)
        
    def retrieve(
        self, 
        query: str,
        filters: Optional[Dict[str, Any]] = None,
        top_k: Optional[int] = None
    ) -> List[RetrievedContext]:
        """
        Retrieve relevant contexts for a query.
        
        Args:
            query: Search query
            filters: Optional metadata filters
            top_k: Override default top_k
            
        Returns:
            List of retrieved contexts
        """
        top_k = top_k or self.config.top_k
        
        # Generate query embedding
        query_embedding = self.embedder.embed_query(query)
        
        # Hybrid search
        results = self.vector_store.hybrid_search(
            query_embedding=query_embedding,
            query_text=query,
            top_k=top_k * 2,  # Retrieve more for re-ranking
            semantic_weight=self.config.semantic_weight,
            keyword_weight=self.config.keyword_weight,
            filters=filters
        )
        
        # Filter by score threshold
        results = [r for r in results if r.score >= self.config.min_score_threshold]
        
        # Deduplicate
        if self.config.deduplicate_results:
            results = self._deduplicate_results(results)
        
        # Re-rank
        reranked = self._rerank(query, query_embedding, results)
        
        # Convert to RetrievedContext
        contexts = []
        for rank, (result, score) in enumerate(reranked):
            context = self._create_context(result, score, rank)
            contexts.append(context)
        
        return contexts[:self.config.final_k]
    
    def build_context_window(
        self, 
        contexts: List[RetrievedContext],
        max_tokens: Optional[int] = None
    ) -> str:
        """
        Build a formatted context window within token budget.
        
        Args:
            contexts: Retrieved contexts
            max_tokens: Maximum tokens for context
            
        Returns:
            Formatted context string
        """
        max_tokens = max_tokens or self.config.max_context_tokens
        
        formatted_parts = []
        current_tokens = 0
        
        for i, context in enumerate(contexts):
            # Estimate tokens
            content_tokens = int(len(context.content) * self.config.tokens_per_char)
            metadata_tokens = 20  # Approximate for source info
            total_tokens = content_tokens + metadata_tokens
            
            if current_tokens + total_tokens > max_tokens:
                break
            
            # Format context
            formatted = context.to_prompt_format(include_metadata=True)
            formatted_parts.append(f"\n--- Context {i+1} ---\n{formatted}")
            
            context.token_count = total_tokens
            context.context_position = i
            current_tokens += total_tokens
        
        return "\n".join(formatted_parts)
    
    def retrieve_with_expansion(
        self,
        query: str,
        expansion_queries: List[str],
        filters: Optional[Dict[str, Any]] = None
    ) -> List[RetrievedContext]:
        """
        Retrieve using query expansion for better coverage.
        
        Args:
            query: Original query
            expansion_queries: Additional query variations
            filters: Optional filters
            
        Returns:
            Merged and ranked contexts
        """
        all_results = []
        
        # Retrieve for original query
        original_results = self.retrieve(query, filters, top_k=self.config.top_k)
        all_results.extend([(r, 1.0) for r in original_results])
        
        # Retrieve for expansion queries
        for exp_query in expansion_queries:
            exp_results = self.retrieve(exp_query, filters, top_k=self.config.top_k // 2)
            all_results.extend([(r, 0.8) for r in exp_results])  # Lower weight for expansions
        
        # Merge and deduplicate
        seen_ids = set()
        merged = []
        
        for result, weight in all_results:
            if result.chunk_id not in seen_ids:
                result.score *= weight
                merged.append(result)
                seen_ids.add(result.chunk_id)
        
        # Sort by score
        merged.sort(key=lambda x: x.score, reverse=True)
        
        return merged[:self.config.final_k]
    
    def _rerank(
        self, 
        query: str, 
        query_embedding: np.ndarray,
        results: List[Any]
    ) -> List[Tuple[Any, float]]:
        """
        Apply re-ranking based on configuration.
        """
        if not results:
            return []
        
        if self.config.reranker_type == ReRankerType.NONE:
            return [(r, r.score) for r in results[:self.config.final_k]]
        
        elif self.config.reranker_type == ReRankerType.CROSS_ENCODER:
            return self.cross_encoder.rerank(query, results, self.config.final_k)
        
        elif self.config.reranker_type == ReRankerType.DIVERSITY:
            # Get embeddings for MMR
            doc_texts = [r.content for r in results]
            doc_embeddings = self.embedder.embed_documents(doc_texts)
            return self.mmr_reranker.rerank(
                query_embedding, results, doc_embeddings, self.config.final_k
            )
        
        elif self.config.reranker_type == ReRankerType.HYBRID:
            # First apply cross-encoder
            cross_ranked = self.cross_encoder.rerank(
                query, results, min(len(results), self.config.final_k * 2)
            )
            
            # Then apply MMR for diversity
            cross_results = [r for r, _ in cross_ranked]
            if len(cross_results) > 1:
                doc_texts = [r.content for r in cross_results]
                doc_embeddings = self.embedder.embed_documents(doc_texts)
                return self.mmr_reranker.rerank(
                    query_embedding, cross_results, doc_embeddings, self.config.final_k
                )
            
            return cross_ranked[:self.config.final_k]
        
        return [(r, r.score) for r in results[:self.config.final_k]]
    
    def _deduplicate_results(self, results: List[Any]) -> List[Any]:
        """Remove duplicate results based on content similarity."""
        unique = []
        seen_content = set()
        
        for result in results:
            # Normalize content for comparison
            normalized = re.sub(r'\s+', ' ', result.content.lower().strip())[:200]
            
            if normalized not in seen_content:
                unique.append(result)
                seen_content.add(normalized)
        
        return unique
    
    def _create_context(
        self, 
        result: Any, 
        score: float, 
        rank: int
    ) -> RetrievedContext:
        """Create RetrievedContext from search result."""
        metadata = result.metadata
        
        # Parse headers
        headers = []
        if 'headers' in metadata:
            try:
                import json
                headers = json.loads(metadata['headers'])
            except:
                pass
        
        return RetrievedContext(
            chunk_id=result.chunk_id,
            content=result.content,
            source_file=result.source_file,
            score=score,
            rank=rank,
            file_type=metadata.get('file_type'),
            start_line=metadata.get('start_line', 0),
            end_line=metadata.get('end_line', 0),
            language=metadata.get('language'),
            headers=headers,
            parent_function=metadata.get('parent_function'),
            parent_class=metadata.get('parent_class'),
        )


class QueryExpander:
    """Expand queries for better retrieval coverage."""
    
    CODE_PATTERNS = {
        "how to": ["implementation of", "example of", "code for"],
        "fix": ["error", "bug", "troubleshoot"],
        "error": ["exception", "traceback", "debug"],
        "function": ["method", "def", "implementation"],
        "class": ["object", "inheritance", "definition"],
    }
    
    @classmethod
    def expand(cls, query: str, max_expansions: int = 3) -> List[str]:
        """
        Generate query expansions.
        
        Args:
            query: Original query
            max_expansions: Maximum number of expansions
            
        Returns:
            List of expanded queries
        """
        expansions = []
        query_lower = query.lower()
        
        for pattern, alternatives in cls.CODE_PATTERNS.items():
            if pattern in query_lower:
                for alt in alternatives[:max_expansions]:
                    expanded = query_lower.replace(pattern, alt)
                    if expanded != query_lower:
                        expansions.append(expanded)
        
        # Add code-specific expansions
        if "python" not in query_lower:
            expansions.append(f"{query} python")
        if "example" not in query_lower:
            expansions.append(f"{query} example code")
        
        return expansions[:max_expansions]


# Example usage
if __name__ == "__main__":
    from embedding_model import NomicEmbedder, EmbeddingConfig
    from vector_store import ChromaVectorStore, VectorStoreConfig
    
    # Initialize components
    embedder = NomicEmbedder(EmbeddingConfig())
    embedder.load_model()
    
    store = ChromaVectorStore(VectorStoreConfig())
    store.initialize()
    
    # Create retrieval engine
    config = RetrievalConfig(
        top_k=10,
        final_k=5,
        reranker_type=ReRankerType.HYBRID,
        max_context_tokens=2048
    )
    engine = RetrievalEngine(store, embedder, config)
    
    # Test query
    query = "How to implement a binary search tree"
    
    # Expand query
    expansions = QueryExpander.expand(query)
    print(f"Query expansions: {expansions}")
    
    # Retrieve
    contexts = engine.retrieve(query)
    
    print(f"\nRetrieved {len(contexts)} contexts:")
    for ctx in contexts:
        print(f"\n[{ctx.rank+1}] Score: {ctx.score:.3f}")
        print(f"Source: {ctx.source_file}")
        print(f"Content: {ctx.content[:150]}...")
    
    # Build context window
    context_window = engine.build_context_window(contexts)
    print(f"\n\nContext window ({len(context_window)} chars):")
    print(context_window[:500] + "...")
