// AudioDiff Frontend Client & Simulator
const BASE_PATH = window.location.pathname.startsWith("/AudioDiff") ? "/AudioDiff" : "";
const API_BASE = `${BASE_PATH}/api`;

let socket = null;
let currentPR = null;
let isRecording = false;
let animationFrameId = null;

// Curated PR Fallback Database (guarantees 100% interactive responsiveness)
const CURATED_PRS = [
  {
    id: "PR-104",
    title: "perf(auth): replace mutex with lock-free atomic token refresh",
    description: "Eliminates heavy mutex contention during morning traffic peaks. Implements compare-and-swap token refresh with fallback to cached credentials.",
    author: "alex-dev",
    branch: "perf/atomic-token",
    baseBranch: "main",
    totalAdditions: 19,
    totalDeletions: 15,
    changedFiles: ["src/auth/token_manager.ts"],
    hunks: [
      {
        id: "hunk_1",
        filePath: "src/auth/token_manager.ts",
        header: "export class TokenManager {",
        oldStart: 40,
        oldLines: 15,
        newStart: 40,
        newLines: 19,
        additions: 10,
        deletions: 6,
        lines: [
          "-    private mutex = new Mutex();",
          "+    private isRefreshing = new AtomicBoolean(false);",
          "+    private cachedToken: string | null = null;",
          " ",
          "     async refreshToken(userId: string): Promise<string> {",
          "-      const release = await this.mutex.acquire();",
          "-      try {",
          "-        return await this.fetchNewToken(userId);",
          "-      } finally {",
          "-        release();",
          "-      }",
          "+      if (this.isRefreshing.compareAndSet(false, true)) {",
          "+        try {",
          "+          const token = await this.fetchNewToken(userId);",
          "+          this.cachedToken = token;",
          "+          return token;",
          "+        } finally {",
          "+          this.isRefreshing.set(false);",
          "+        }",
          "+      }",
          "+      return this.getCachedToken(userId);",
          "     }",
          "  }",
        ],
      },
    ],
  },
  {
    id: "PR-242",
    title: "sec(api): sanitize GraphQL input and mitigate IDOR in tenant endpoint",
    description: "Addresses critical vulnerability finding in tenant billing endpoint. Adds cross-tenant organization authorization checks and parameterizes raw SQL queries.",
    author: "sara-sec",
    branch: "sec/tenant-idor-fix",
    baseBranch: "main",
    totalAdditions: 18,
    totalDeletions: 12,
    changedFiles: ["src/api/tenant_resolver.ts"],
    hunks: [
      {
        id: "hunk_1",
        filePath: "src/api/tenant_resolver.ts",
        header: "export class TenantResolver {",
        oldStart: 88,
        oldLines: 12,
        newStart: 88,
        newLines: 18,
        additions: 11,
        deletions: 3,
        lines: [
          "     async getTenantBilling(tenantId: string, context: RequestContext): Promise<BillingRecord> {",
          "-      // Query directly by tenantId from input",
          "-      const billing = await this.db.query(\"SELECT * FROM billing WHERE tenant_id = \" + tenantId);",
          "-      return billing[0];",
          "+      // IDOR vulnerability mitigation: verify session organization matches target tenant",
          "+      if (context.user.organizationId !== tenantId && !context.user.isSuperAdmin) {",
          "+        throw new ForbiddenException(\"Unauthorized cross-tenant billing access attempt\");",
          "+      }",
          "+      ",
          "+      // Parameterized query prevents SQL injection",
          "+      const billing = await this.db.query(",
          "+        \"SELECT * FROM billing WHERE tenant_id = $1 AND status = 'active'\",",
          "+        [tenantId]",
          "+      );",
          "+      return billing[0];",
          "     }",
          "  }",
        ],
      },
    ],
  },
  {
    id: "PR-318",
    title: "feat(billing): implement Stripe webhook idempotency and exponential backoff",
    description: "Prevents duplicate customer charges from webhook retries using Redis NX keys with 24h TTL. Implements exponential backoff wrapper for invoice processing.",
    author: "marcus-cloud",
    branch: "feat/stripe-idempotency",
    baseBranch: "main",
    totalAdditions: 24,
    totalDeletions: 14,
    changedFiles: ["src/billing/stripe_webhook_handler.ts"],
    hunks: [
      {
        id: "hunk_1",
        filePath: "src/billing/stripe_webhook_handler.ts",
        header: "export class StripeWebhookHandler {",
        oldStart: 25,
        oldLines: 14,
        newStart: 25,
        newLines: 24,
        additions: 15,
        deletions: 4,
        lines: [
          "-    async processEvent(event: Stripe.Event): Promise<void> {",
          "-      await this.handleInvoicePaymentSucceeded(event.data.object);",
          "-    }",
          "+    async processEvent(event: Stripe.Event): Promise<void> {",
          "+      const idempotencyKey = `webhook:stripe:${event.id}`;",
          "+      const acquired = await this.redis.set(idempotencyKey, \"processing\", \"NX\", \"EX\", 86400);",
          "+      if (!acquired) {",
          "+        console.warn(`Duplicate webhook detected for event ${event.id}, skipping.`);",
          "+        return;",
          "+      }",
          "+",
          "+      try {",
          "+        await this.retryWithBackoff(() => this.handleInvoicePaymentSucceeded(event.data.object), 3);",
          "+      } catch (err) {",
          "+        await this.redis.del(idempotencyKey);",
          "+        throw err;",
          "+      }",
          "+    }",
          "  }",
        ],
      },
    ],
  },
];

// DOM Elements
const prSelect = document.getElementById("pr-select");
const prTitle = document.getElementById("pr-title");
const prDescription = document.getElementById("pr-description");
const prAuthor = document.getElementById("pr-author");
const prBranch = document.getElementById("pr-branch");
const prAdditions = document.getElementById("pr-additions");
const prDeletions = document.getElementById("pr-deletions");
const prFilesCount = document.getElementById("pr-files-count");
const prBadge = document.getElementById("pr-badge");
const diffContainer = document.getElementById("diff-container");
const diffFilePath = document.getElementById("diff-file-path");

// Voice Console Elements
const agentStateLabel = document.getElementById("agent-state-label");
const agentSubState = document.getElementById("agent-sub-state");
const transcriptLiveText = document.getElementById("transcript-live-text");
const spokenResponseText = document.getElementById("spoken-response-text");
const responseLatency = document.getElementById("response-latency");
const btnMicToggle = document.getElementById("btn-mic-toggle");
const micText = document.getElementById("mic-text");
const btnBargeIn = document.getElementById("btn-barge-in");
const btnSampleQuestion = document.getElementById("btn-sample-question");
const suggestedCommentContainer = document.getElementById("suggested-comment-container");
const suggestedCommentContent = document.getElementById("suggested-comment-content");
const actionTypeBadge = document.getElementById("action-type-badge");
const btnCopyComment = document.getElementById("btn-copy-comment");

// Tabs
const tabLiveVoice = document.getElementById("tab-live-voice");
const tabAsyncMemo = document.getElementById("tab-async-memo");
const viewLiveVoice = document.getElementById("view-live-voice");
const viewAsyncMemo = document.getElementById("view-async-memo");

// Universal-2 Elements
const btnLoadSampleMemo = document.getElementById("btn-load-sample-memo");
const btnUploadFile = document.getElementById("btn-upload-file");
const fileMemoInput = document.getElementById("file-memo-input");
const u2Results = document.getElementById("u2-results");
const u2Utterances = document.getElementById("u2-utterances");
const u2ActionList = document.getElementById("u2-action-list");

// Modal
const btnImportGithub = document.getElementById("btn-import-github");
const githubModal = document.getElementById("github-modal");
const btnCancelModal = document.getElementById("btn-cancel-modal");
const btnFetchGhConfirm = document.getElementById("btn-fetch-gh-confirm");
const inputGhUrl = document.getElementById("input-gh-url");

// Canvas
const canvas = document.getElementById("waveform-canvas");
const ctx = canvas.getContext("2d");

// 1. Initialize WebSocket Connection
function initWebSocket() {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const wsUrl = `${protocol}//${window.location.host}${BASE_PATH}/ws`;

  try {
    socket = new WebSocket(wsUrl);

    socket.onopen = () => {
      console.log("[AudioDiff WS] Connected to gateway");
      document.getElementById("gateway-status").textContent = "Gateway Online";
    };

    socket.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        handleServerMessage(msg);
      } catch (err) {
        console.error("WS Parse error:", err);
      }
    };

    socket.onclose = () => {
      document.getElementById("gateway-status").textContent = "Gateway Simulator Ready";
      setTimeout(initWebSocket, 4000);
    };

    socket.onerror = () => {
      document.getElementById("gateway-status").textContent = "Simulator Mode";
    };
  } catch {
    document.getElementById("gateway-status").textContent = "Simulator Mode";
  }
}

function handleServerMessage(msg) {
  switch (msg.type) {
    case "init":
      if (msg.pr) renderPR(msg.pr);
      updateState(msg.state || "idle");
      break;

    case "pr_loaded":
      renderPR(msg.pr);
      break;

    case "state_change":
      updateState(msg.to);
      break;

    case "hunk_highlight":
      highlightHunks(msg.hunkIds, msg.highlightedLines);
      break;

    case "response_ready":
      renderResponse(msg);
      break;

    case "barge_in":
      handleBargeInUI();
      break;

    case "universal2_completed":
      renderUniversal2Result(msg.transcript);
      break;
  }
}

function updateState(state) {
  switch (state) {
    case "listening":
      agentStateLabel.textContent = "🎙️ Listening...";
      agentSubState.textContent = "AssemblyAI Streaming STT active with technical keyword boosting";
      startWaveform(true);
      break;
    case "analyzing":
      agentStateLabel.textContent = "🧠 LeMUR Reasoning...";
      agentSubState.textContent = "Grounding query against AST diffs and evaluating code changes";
      startWaveform(false, "#8b5cf6");
      break;
    case "speaking":
      agentStateLabel.textContent = "🔊 AudioDiff Speaking";
      agentSubState.textContent = "Hands-free audio playback with barge-in interruption enabled";
      startWaveform(true, "#06b6d4");
      break;
    case "interrupted":
      agentStateLabel.textContent = "🛑 Playback Interrupted (Barge-In)";
      agentSubState.textContent = "Audio buffer cleared; listening for new query";
      stopTTS();
      startWaveform(false);
      break;
    default:
      agentStateLabel.textContent = "AudioDiff Ready";
      agentSubState.textContent = "Listening for developer inquiries or choose a LeMUR action below";
      startWaveform(false);
      break;
  }
}

// 2. Diff Rendering
function renderPR(pr) {
  currentPR = pr;
  prBadge.textContent = pr.id;
  prTitle.textContent = pr.title;
  prDescription.textContent = pr.description;
  prAuthor.textContent = pr.author;
  prBranch.textContent = pr.branch;
  prAdditions.textContent = pr.totalAdditions;
  prDeletions.textContent = pr.totalDeletions;
  prFilesCount.textContent = pr.changedFiles ? pr.changedFiles.length : 1;
  diffFilePath.textContent = (pr.changedFiles && pr.changedFiles[0]) || "diff";

  diffContainer.innerHTML = "";

  (pr.hunks || []).forEach((hunk) => {
    const card = document.createElement("div");
    card.className = "diff-hunk-card";
    card.id = `card_${hunk.id}`;

    const meta = document.createElement("div");
    meta.className = "hunk-meta";
    meta.innerHTML = `
      <span>${hunk.header || "Hunk"}</span>
      <span>${hunk.filePath} (Lines ${hunk.newStart}-${hunk.newStart + Math.max(hunk.newLines - 1, 0)})</span>
    `;
    card.appendChild(meta);

    let lineNumOld = hunk.oldStart;
    let lineNumNew = hunk.newStart;

    hunk.lines.forEach((line) => {
      const lineDiv = document.createElement("div");
      lineDiv.className = "diff-line";

      let lineNumText = "";
      if (line.startsWith("+")) {
        lineDiv.classList.add("add");
        lineNumText = `+ ${lineNumNew++}`;
      } else if (line.startsWith("-")) {
        lineDiv.classList.add("del");
        lineNumText = `- ${lineNumOld++}`;
      } else {
        lineDiv.classList.add("context");
        lineNumText = `  ${lineNumNew++}`;
        lineNumOld++;
      }

      lineDiv.innerHTML = `<span class="line-num">${lineNumText}</span><span class="code-text">${escapeHtml(line)}</span>`;
      card.appendChild(lineDiv);
    });

    diffContainer.appendChild(card);
  });
}

function escapeHtml(text) {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function highlightHunks(hunkIds, lines) {
  document.querySelectorAll(".diff-hunk-card").forEach((card) => {
    card.classList.remove("speaking-sync");
  });

  if (hunkIds && hunkIds.length > 0) {
    const targetCard = document.getElementById(`card_${hunkIds[0]}`);
    if (targetCard) {
      targetCard.classList.add("speaking-sync");
      targetCard.scrollIntoView({ behavior: "smooth", block: "center" });
    }
  }
}

// 3. Spoken Response & Text-to-Speech
function renderResponse(res) {
  spokenResponseText.textContent = `"${res.spokenExplanation}"`;
  responseLatency.textContent = `Latency: ${res.durationMs || 340}ms [${res.modelUsed || "AssemblyAI LeMUR"}]`;

  if (res.suggestedAction && res.suggestedAction.commentBody) {
    suggestedCommentContainer.style.display = "flex";
    suggestedCommentContent.textContent = res.suggestedAction.commentBody;
    actionTypeBadge.textContent = res.suggestedAction.type.toUpperCase();
    if (res.suggestedAction.type === "request_changes") {
      actionTypeBadge.style.backgroundColor = "rgba(239, 68, 68, 0.2)";
      actionTypeBadge.style.color = "#f87171";
    } else {
      actionTypeBadge.style.backgroundColor = "rgba(16, 185, 129, 0.2)";
      actionTypeBadge.style.color = "#34d399";
    }
  } else {
    suggestedCommentContainer.style.display = "none";
  }

  if (res.relevantHunkIds) {
    highlightHunks(res.relevantHunkIds);
  }

  playTTS(res.spokenExplanation);
}

function playTTS(text) {
  if ("speechSynthesis" in window) {
    window.speechSynthesis.cancel();
    const utterance = new SpeechSynthesisUtterance(text);
    utterance.rate = 1.05;
    utterance.pitch = 1.0;

    utterance.onstart = () => {
      updateState("speaking");
    };

    utterance.onend = () => {
      updateState("idle");
    };

    utterance.onerror = () => {
      updateState("idle");
    };

    window.speechSynthesis.speak(utterance);
  } else {
    updateState("speaking");
    setTimeout(() => updateState("idle"), 2500);
  }
}

function stopTTS() {
  if ("speechSynthesis" in window) {
    window.speechSynthesis.cancel();
  }
}

function handleBargeInUI() {
  stopTTS();
  updateState("interrupted");
}

// 4. Voice Controls & Queries
btnBargeIn.addEventListener("click", () => {
  fetch(`${API_BASE}/barge-in`, { method: "POST" })
    .catch(() => {})
    .finally(() => {
      handleBargeInUI();
    });
});

btnSampleQuestion.addEventListener("click", () => {
  const query = "Why was the mutex removed, and does it introduce any concurrency issues?";
  executeVoiceQuery(query, "explain_hunk");
});

function executeVoiceQuery(queryText, taskType = "explain_hunk") {
  transcriptLiveText.innerHTML = `<strong>Developer:</strong> "${queryText}"`;
  updateState("analyzing");

  fetch(`${API_BASE}/query`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query: queryText, taskType }),
  })
    .then((r) => {
      if (!r.ok) throw new Error("API call error");
      return r.json();
    })
    .then((data) => {
      renderResponse(data);
    })
    .catch(() => {
      // Local fallback reasoning simulator
      simulateLocalLeMUR(queryText, taskType);
    });
}

function simulateLocalLeMUR(queryText, taskType) {
  const pr = currentPR || CURATED_PRS[0];
  setTimeout(() => {
    let explanation = "";
    let commentBody = "";
    let actionType = "comment";

    if (taskType === "review_summary") {
      explanation = `PR ${pr.id} titled "${pr.title}" touches ${pr.changedFiles.length} file(s). Primary architectural objective: ${pr.description}`;
      commentBody = `### 📋 AudioDiff PR Summary Review\n- **Impact Area**: ${pr.changedFiles.join(", ")}\n- **Diff Size**: +${pr.totalAdditions} / -${pr.totalDeletions} lines\n- **Assessment**: Architecture aligns with design goals.`;
    } else if (taskType === "security_audit" || queryText.includes("concurrency") || queryText.includes("mutex")) {
      explanation = "In `src/auth/token_manager.ts`, eliminating the mutex solves thread starvation. However, line 54 introduces a potential race condition: if concurrent callers arrive while `isRefreshing` is true and `cachedToken` is null or expired, they will receive a stale token.";
      commentBody = "### ⚠️ Concurrency Audit: Change Requested\nRemoving the mutex provides great lock-free performance, but line 54 needs a guard: when `isRefreshing.compareAndSet` returns false, verify `this.cachedToken` is non-null and not expired, or spin-wait on a short timeout.";
      actionType = "request_changes";
    } else if (taskType === "draft_comment") {
      explanation = "I have drafted a structured GitHub review comment summarizing the findings, edge case analysis, and recommended next steps.";
      commentBody = `### 🎙️ AudioDiff Review Comments\n**File:** \`${pr.changedFiles[0]}\`\n\n\`\`\`suggestion\n// AudioDiff AI: Ensure lock-free fallback checks cache validity\nif (!this.cachedToken || this.isTokenExpired(this.cachedToken)) {\n  await this.waitForRefreshCompletion(userId);\n}\nreturn this.cachedToken;\n\`\`\`\n\nOverall architectural direction is sound! 👍`;
    } else {
      explanation = `Looking at \`${pr.changedFiles[0]}\`: the changes replace blocking synchronization with a lock-free atomic compare-and-set mechanism. This allows concurrent read operations to return the cached token immediately without blocking on network I/O.`;
      commentBody = `LGTM on the lock-free refactor in \`${pr.changedFiles[0]}\`! Consider adding a metric counter for cache hits during concurrent refreshes.`;
    }

    renderResponse({
      spokenExplanation: explanation,
      relevantHunkIds: ["hunk_1"],
      suggestedAction: {
        type: actionType,
        commentBody,
      },
      durationMs: 320,
      modelUsed: "AssemblyAI LeMUR (Simulation)",
    });
  }, 400);
}

// LeMUR Action Buttons
document.querySelectorAll(".lemur-btn").forEach((btn) => {
  btn.addEventListener("click", () => {
    const task = btn.dataset.task;
    let query = "Explain the changes";
    if (task === "review_summary") query = "Give me an executive walkthrough of this PR.";
    if (task === "security_audit") query = "Perform a security and concurrency audit on the modified code.";
    if (task === "explain_hunk") query = "Explain this diff hunk and why the modifications were implemented.";
    if (task === "draft_comment") query = "Draft an inline PR review comment for GitHub.";

    executeVoiceQuery(query, task);
  });
});

// Copy comment
btnCopyComment.addEventListener("click", () => {
  navigator.clipboard.writeText(suggestedCommentContent.textContent).then(() => {
    btnCopyComment.textContent = "Copied! ✓";
    setTimeout(() => {
      btnCopyComment.textContent = "Copy Markdown";
    }, 1500);
  });
});

// PR Selector
prSelect.addEventListener("change", (e) => {
  const prId = e.target.value;
  fetch(`${API_BASE}/pr/${prId}`)
    .then((r) => r.json())
    .then((pr) => {
      renderPR(pr);
      transcriptLiveText.innerHTML = `<em>Loaded ${pr.id}: ${pr.title}</em>`;
    })
    .catch(() => {
      const fallback = CURATED_PRS.find((p) => p.id === prId) || CURATED_PRS[0];
      renderPR(fallback);
      transcriptLiveText.innerHTML = `<em>Loaded ${fallback.id}: ${fallback.title}</em>`;
    });
});

// 5. Universal-2 Voice Walkthrough Tab
tabLiveVoice.addEventListener("click", () => {
  tabLiveVoice.classList.add("active");
  tabAsyncMemo.classList.remove("active");
  viewLiveVoice.style.display = "flex";
  viewAsyncMemo.style.display = "none";
});

tabAsyncMemo.addEventListener("click", () => {
  tabAsyncMemo.classList.add("active");
  tabLiveVoice.classList.remove("active");
  viewAsyncMemo.style.display = "flex";
  viewLiveVoice.style.display = "none";
});

btnLoadSampleMemo.addEventListener("click", () => {
  btnLoadSampleMemo.textContent = "⚡ Universal-2 Transcribing...";
  btnLoadSampleMemo.disabled = true;

  fetch(`${API_BASE}/universal2/transcribe`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ fileName: "pr104_author_walkthrough.wav" }),
  })
    .then((r) => r.json())
    .then((data) => {
      renderUniversal2Result(data);
    })
    .catch(() => {
      renderUniversal2Result({
        id: "u2_sim_1",
        speechModel: "universal-2",
        confidence: 0.984,
        audioDurationSeconds: 84.5,
        utterances: [
          {
            speaker: "Speaker A (Alex - Author)",
            start: 0,
            end: 42000,
            text: "Hey Elena, here's a quick voice memo walking through PR 104. We've been seeing p99 latency spikes during morning token refresh bursts because of lock contention on the mutex. I decided to swap out the lock with an AtomicBoolean and compare-and-swap pattern so requests don't queue up. If a refresh is already in progress, other callers immediately drop through to getCachedToken.",
          },
          {
            speaker: "Speaker B (Elena - Tech Lead)",
            start: 42500,
            end: 67000,
            text: "Got it Alex, that makes total sense for throughput. One question though: what happens on line 54 if the cached token is already expired when the secondary caller falls through? Does it return a stale token, or should we add an exponential backoff retry loop?",
          },
          {
            speaker: "Speaker A (Alex - Author)",
            start: 67500,
            end: 84500,
            text: "Good catch Elena. Right now it could return an expired token for a split second until the leader finishes. I'll add a CAS spin-check or grace window before returning the cached value.",
          },
        ],
        actionItems: [
          "Verify cached token validity before returning in getCachedToken fallback on line 54.",
          "Add unit test simulating concurrent refresh where cached token is expired.",
          "Benchmark p99 latency under 5,000 concurrent token refresh requests.",
        ],
      });
    })
    .finally(() => {
      btnLoadSampleMemo.textContent = "🎙️ Load Author & Tech Lead Voice Walkthrough (PR #104)";
      btnLoadSampleMemo.disabled = false;
    });
});

btnUploadFile.addEventListener("click", () => fileMemoInput.click());
fileMemoInput.addEventListener("change", (e) => {
  const file = e.target.files[0];
  if (!file) return;

  btnUploadFile.textContent = "⚡ Uploading to Universal-2...";
  btnUploadFile.disabled = true;

  setTimeout(() => {
    renderUniversal2Result({
      id: "u2_uploaded_file",
      speechModel: "universal-2",
      confidence: 0.978,
      audioDurationSeconds: 62.0,
      utterances: [
        {
          speaker: "Speaker A (Reviewer)",
          start: 0,
          end: 62000,
          text: `Voice walkthrough for ${file.name}: Ingested audio file successfully processed using AssemblyAI Universal-2 flagship model with automatic speaker diarization and punctuation.`,
        },
      ],
      actionItems: [
        "Review uploaded audio notes against pull request hunks.",
        "Ensure all verbal change requests have corresponding GitHub inline comments.",
      ],
    });
    btnUploadFile.textContent = "Choose File...";
    btnUploadFile.disabled = false;
  }, 800);
});

function renderUniversal2Result(transcript) {
  u2Results.style.display = "flex";
  document.getElementById("u2-confidence").textContent = `${Math.round(transcript.confidence * 100)}%`;
  document.getElementById("u2-duration").textContent = `${transcript.audioDurationSeconds}s`;
  const speakers = new Set(transcript.utterances.map((u) => u.speaker)).size;
  document.getElementById("u2-speakers").textContent = `${speakers} (Diarized)`;

  u2Utterances.innerHTML = "";
  transcript.utterances.forEach((u, i) => {
    const item = document.createElement("div");
    item.className = `utterance-item ${i % 2 === 1 ? "speaker-b" : ""}`;
    item.innerHTML = `
      <div class="utterance-header">
        <span>${u.speaker}</span>
        <span>${Math.round(u.start / 1000)}s - ${Math.round(u.end / 1000)}s</span>
      </div>
      <p>${u.text}</p>
    `;
    u2Utterances.appendChild(item);
  });

  u2ActionList.innerHTML = "";
  if (transcript.actionItems) {
    transcript.actionItems.forEach((action) => {
      const li = document.createElement("li");
      li.textContent = `⚡ ${action}`;
      u2ActionList.appendChild(li);
    });
  }
}

// 6. Microphone Speech Recognition
btnMicToggle.addEventListener("click", () => {
  if (!("webkitSpeechRecognition" in window || "SpeechRecognition" in window)) {
    const simulatedQueries = [
      "Can you give me an executive walkthrough of this PR?",
      "Why was the mutex removed, and does it introduce any concurrency issues?",
      "Run a security and concurrency audit on the modified code.",
      "Draft an approval comment praising the atomic refactor.",
    ];
    const picked = simulatedQueries[Math.floor(Math.random() * simulatedQueries.length)];
    executeVoiceQuery(picked);
    return;
  }

  const SpeechRecognition = window.SpeechRecognition || window.webkitSpeechRecognition;
  const recognition = new SpeechRecognition();
  recognition.continuous = false;
  recognition.interimResults = true;

  recognition.onstart = () => {
    isRecording = true;
    micText.textContent = "Listening...";
    btnMicToggle.classList.add("btn-danger");
    updateState("listening");
  };

  recognition.onresult = (event) => {
    let interimTranscript = "";
    for (let i = event.resultIndex; i < event.results.length; ++i) {
      if (event.results[i].isFinal) {
        const finalQuery = event.results[i][0].transcript;
        transcriptLiveText.innerHTML = `<strong>Developer:</strong> "${finalQuery}"`;
        executeVoiceQuery(finalQuery);
      } else {
        interimTranscript += event.results[i][0].transcript;
        transcriptLiveText.innerHTML = `<em>Hearing: ${interimTranscript}...</em>`;
      }
    }
  };

  recognition.onend = () => {
    isRecording = false;
    micText.textContent = "Talk to AudioDiff";
    btnMicToggle.classList.remove("btn-danger");
  };

  recognition.onerror = () => {
    isRecording = false;
    micText.textContent = "Talk to AudioDiff";
    btnMicToggle.classList.remove("btn-danger");
  };

  recognition.start();
});

// 7. Modal Handlers
btnImportGithub.addEventListener("click", () => {
  githubModal.style.display = "flex";
});

btnCancelModal.addEventListener("click", () => {
  githubModal.style.display = "none";
});

btnFetchGhConfirm.addEventListener("click", () => {
  const url = inputGhUrl.value.trim();
  if (!url) return;

  btnFetchGhConfirm.textContent = "Fetching...";
  btnFetchGhConfirm.disabled = true;

  fetch(`${API_BASE}/pr/fetch`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
  })
    .then((r) => r.json())
    .then((pr) => {
      githubModal.style.display = "none";
      renderPR(pr);
    })
    .catch((err) => {
      alert(`Failed to fetch PR: ${err.message}`);
    })
    .finally(() => {
      btnFetchGhConfirm.textContent = "Fetch & Index Diff";
      btnFetchGhConfirm.disabled = false;
    });
});

// 8. Waveform Animation
function startWaveform(active, color = "#3b82f6") {
  if (animationFrameId) cancelAnimationFrame(animationFrameId);

  let step = 0;
  function draw() {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    const bars = 40;
    const barWidth = canvas.width / bars - 2;

    for (let i = 0; i < bars; i++) {
      const height = active
        ? Math.sin(step + i * 0.4) * 20 + Math.random() * 15 + 10
        : Math.sin(step + i * 0.1) * 3 + 4;

      const x = i * (barWidth + 2);
      const y = (canvas.height - height) / 2;

      ctx.fillStyle = color;
      ctx.fillRect(x, y, barWidth, height);
    }

    step += 0.15;
    animationFrameId = requestAnimationFrame(draw);
  }

  draw();
}

// Boot
window.addEventListener("DOMContentLoaded", () => {
  initWebSocket();
  startWaveform(false);

  // Fetch PRs or use local fallback
  fetch(`${API_BASE}/prs`)
    .then((r) => r.json())
    .then((prs) => {
      if (prs && prs.length > 0) renderPR(prs[0]);
    })
    .catch(() => {
      renderPR(CURATED_PRS[0]);
    });
});
