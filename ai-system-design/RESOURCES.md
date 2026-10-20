# System Design for AI Engineers Resources

## Knowledge

- [Book: _Designing Machine Learning Systems_ by Chip Huyen (O'Reilly, 2022)](https://www.oreilly.com/library/view/designing-machine-learning/9781098107956/)
  Definitive guide on end-to-end production ML systems. Use for: requirements scoping, data engineering, model serving patterns, offline vs. online evaluation, and continual learning architectures.
- [Paper: "Efficient Memory Management for Large Language Model Serving with PagedAttention" by Woosuk Kwon et al. (vLLM / SOSP 2023)](https://arxiv.org/abs/2309.06180)
  Seminal research on serving throughput and KV cache bottlenecks. Use for: LLM serving architecture, continuous batching, memory fragmentation, and vLLM design.
- [Article: "Patterns for Building LLM-based Systems & Products" by Eugene Yan](https://eugeneyan.com/writing/llm-patterns/)
  Battle-tested industry architectural patterns. Use for: practical RAG architectures, multi-stage retrieval, caching, fine-tuning vs. prompt engineering, and guardrails.
- [Engineering Guide: "Building Effective Agents" by Anthropic](https://www.anthropic.com/research/building-effective-agents)
  Pragmatic taxonomy of LLM agents and multi-agent workflows. Use for: workflow patterns (routing, chaining, parallelization, orchestrator-workers) vs. autonomous agent architectures.
- [Guide: "Rules of Machine Learning: Best Practices for ML Engineering" by Martin Zinkevich (Google)](https://developers.google.com/machine-learning/guides/rules-of-ml)
  Pragmatic engineering rules from Google. Use for: pipeline design, metric tracking, debugging data pipelines, and avoiding premature complexity.
- [Paper: "Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks" by Patrick Lewis et al. (NeurIPS 2020)](https://arxiv.org/abs/2005.11401)
  Foundational academic basis for RAG. Use for: architectural separation of parametric vs. non-parametric memory.

## Wisdom (Communities)

- [MLOps Community](https://mlops.community/)
  Premier global community of 25,000+ engineers, podcasts, and Discord channels. Use for: real-world production postmortems, architecture reviews, and tooling comparisons.
- [Latent Space Community & Podcast](https://www.latent.space/)
  Leading community for AI engineers and founders. Use for: discussions on frontier AI infra, agent frameworks, eval harnesses, and emerging system patterns.
- [r/LocalLLaMA](https://www.reddit.com/r/LocalLLaMA/)
  High-signal practical forum for model serving, quantization, hardware tradeoffs, and inference engines. Use for: hardware sizing benchmarks, quantization edge cases, and serving engine trade-offs.

## Gaps

- Comprehensive Staff-level back-of-the-envelope estimation cheat sheets specifically for LLM serving (KV cache memory, TTFT vs. TPOT calculations, cluster sizing under strict SLAs). Will be synthesized in reference documents during this track.
