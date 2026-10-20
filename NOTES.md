# Workspace Notes

## Orchestration Rule
- **Orchestrator Rule**: The primary agent must NEVER write or generate code directly; it acts strictly as Orchestrator, Reviewer, and System Designer, delegating all code/HTML/script implementation to a `flash` subagent.

- **Initial Request**: Relearn Mandarin after ~7 years of disuse (previously passed at least HSK 3) to speak technically with a Chinese superior as an AIOps Engineer at Huawei.
- **Learner Profile**:
  - **Prior Mandarin Level**: Passed HSK 3 (~600 core words, foundational grammar structures) ~7 years ago. High storage strength on core sentence skeletons, low current retrieval fluency due to disuse.
  - **Domain Expertise**: AIOps Engineer at Huawei (observability, anomaly detection, alert noise reduction, root cause analysis, ML/LLM operations).
  - **Primary Use Case**: Spoken technical communication (status updates, incident briefings, RCA explanations, model/threshold discussions) with a Chinese manager/superior.
- **Voice & Real-Time Speaking Preferences**:
  - User wants real-time pronunciation review and conversational practice similar to Gemini Live voice chat.
  - **Hybrid Voice Strategy**:
    1. Every lesson includes a ready-to-copy **Gemini Live Voice Call System Prompt** scoped to that lesson's vocabulary and sentence structures, so the user can paste it into Gemini Live on mobile/web for low-latency voice roleplay with tone correction.
    2. Every lesson's speaking drills include an in-browser **Microphone Pronunciation & Character-Diff Checker** (`webkitSpeechRecognition` `zh-CN` + self-recording playback) for sentence-by-sentence pronunciation verification.
- **Pedagogical Guidelines**:
  1. **Always include Hanzi + Pinyin (with tone marks) + Literal Structure**: Never let character recognition rust slow down spoken sentence construction.
  2. **Decompose 2-character & 4-character compounds**: Connect new AIOps terms (`根因分析`, `告警降噪`, `故障定位`, `误报`) to familiar HSK 2/3 character roots (`根` root, `因` cause/因为, `报` report/报纸, `错/误` error).
  3. **Upgrade HSK 3 skeletons rather than teaching from scratch**: Anchor every technical pattern on a rusty HSK 3 structure the learner already owns (e.g., `因为...所以...` &rarr; `由于...导致...`).
  4. **Audio + Retrieval Practice**: Include browser speech synthesis (`zh-CN`), mic recognition diffing, and a Gemini Live roleplay prompt in every lesson.
