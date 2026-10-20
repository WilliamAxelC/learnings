import { ToolCall } from "./tools.js";

export interface SpeculativeDraft {
  predictedStdout: string;
  predictedExitCode: number;
  speculativeNextAction: {
    thought: string;
    actionType: "tool_call" | "final_response";
    nextTool?: ToolCall;
    responseContent?: string;
  };
  confidence: number;
  draftLatencyMs: number;
}

export class System1DraftEngine {
  private baseUrl: string;
  private apiKey: string;
  private model: string;
  private simulationMode: boolean;

  constructor() {
    this.baseUrl = process.env.ROCM_VLLM_BASE_URL || "http://localhost:8000/v1";
    this.apiKey = process.env.ROCM_VLLM_API_KEY || "local-key";
    this.model = process.env.SYSTEM1_MODEL || "Qwen/Qwen2.5-Coder-1.5B-Instruct";
    this.simulationMode = process.env.SIMULATION_MODE !== "false";
  }

  /**
   * Reflexively predicts tool output and drafts next agent action in sub-25ms
   */
  async draftNextStep(
    taskDescription: string,
    toolCall: ToolCall,
    recentHistory: string[]
  ): Promise<SpeculativeDraft> {
    const start = performance.now();

    if (this.simulationMode) {
      // Simulate ultra-fast ROCm vLLM token generation (15-25ms)
      await new Promise((r) => setTimeout(r, 18));

      // Heuristic prediction based on tool intent
      let predictedStdout = "Command completed successfully.";
      let predictedExitCode = 0;
      let nextAction: SpeculativeDraft["speculativeNextAction"] = {
        thought: "Tool completed as expected. Proceeding with planned file update.",
        actionType: "tool_call",
        nextTool: {
          id: `call_${Date.now()}`,
          name: "write_file",
          args: { path: "src/utils.ts", content: "// Speculatively generated patch" },
        },
      };

      if (toolCall.args.command?.includes("test")) {
        predictedStdout = "PASS src/index.test.ts (4 tests passed, 0 failed)";
        nextAction = {
          thought: "Tests passed. Creating git commit for the feature.",
          actionType: "tool_call",
          nextTool: {
            id: `call_${Date.now()}`,
            name: "bash",
            args: { command: "git add . && git commit -m 'feat: complete implementation'" },
          },
        };
      } else if (toolCall.args.command?.includes("lint")) {
        predictedStdout = "No linting errors found.";
        nextAction = {
          thought: "Linter is clean. Ready to trigger test suite.",
          actionType: "tool_call",
          nextTool: {
            id: `call_${Date.now()}`,
            name: "bash",
            args: { command: "pnpm test" },
          },
        };
      }

      const draftLatencyMs = Math.round(performance.now() - start);
      return {
        predictedStdout,
        predictedExitCode,
        speculativeNextAction: nextAction,
        confidence: 0.92,
        draftLatencyMs,
      };
    }

    // Live vLLM ROCm API call
    try {
      const prompt = `You are a high-speed System 1 predictor running on AMD ROCm.
Task: ${taskDescription}
Dispatched Tool: ${toolCall.name} (${JSON.stringify(toolCall.args)})

Predict the tool stdout and the immediate next agent action in JSON:
{
  "predictedStdout": string,
  "predictedExitCode": number,
  "nextThought": string,
  "nextToolName": string,
  "nextToolArgs": object
}`;

      const res = await fetch(`${this.baseUrl}/chat/completions`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${this.apiKey}`,
        },
        body: JSON.stringify({
          model: this.model,
          messages: [{ role: "user", content: prompt }],
          temperature: 0.1,
          max_tokens: 200,
        }),
      });

      const data = (await res.json()) as any;
      const content = data.choices?.[0]?.message?.content || "{}";
      const parsed = JSON.parse(content.match(/\{[\s\S]*\}/)?.[0] || "{}");

      const draftLatencyMs = Math.round(performance.now() - start);
      return {
        predictedStdout: parsed.predictedStdout || "Success",
        predictedExitCode: parsed.predictedExitCode ?? 0,
        speculativeNextAction: {
          thought: parsed.nextThought || "Continuing task",
          actionType: "tool_call",
          nextTool: {
            id: `call_${Date.now()}`,
            name: parsed.nextToolName || "bash",
            args: parsed.nextToolArgs || {},
          },
        },
        confidence: 0.88,
        draftLatencyMs,
      };
    } catch {
      // Fallback
      return {
        predictedStdout: "Success",
        predictedExitCode: 0,
        speculativeNextAction: {
          thought: "Continuing workflow",
          actionType: "final_response",
          responseContent: "Task complete",
        },
        confidence: 0.5,
        draftLatencyMs: Math.round(performance.now() - start),
      };
    }
  }
}
