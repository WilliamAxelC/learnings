import { ToolCall, ToolResult } from "./tools.js";

export interface RecoveryPlan {
  divergenceAnalysis: string;
  chainOfThought: string;
  recoveredAction: {
    thought: string;
    nextTool?: ToolCall;
    responseContent?: string;
  };
  durationMs: number;
}

export class System2ReasoningEngine {
  private baseUrl: string;
  private apiKey: string;
  private model: string;
  private simulationMode: boolean;

  constructor() {
    this.baseUrl = process.env.ROCM_VLLM_BASE_URL || "http://localhost:8000/v1";
    this.apiKey = process.env.ROCM_VLLM_API_KEY || "local-key";
    this.model = process.env.SYSTEM2_MODEL || "deepseek-ai/DeepSeek-R1-Distill-Qwen-32B";
    this.simulationMode = process.env.SIMULATION_MODE !== "false";
  }

  /**
   * Invoked upon speculation divergence or tool error for deep deliberative reasoning
   */
  async deliberateRecovery(
    taskDescription: string,
    toolCall: ToolCall,
    actualResult: ToolResult,
    predictedStdout: string
  ): Promise<RecoveryPlan> {
    const start = performance.now();

    if (this.simulationMode) {
      // Simulate System 2 deliberative reasoning (e.g. 350ms on AMD Instinct)
      await new Promise((r) => setTimeout(r, 220));

      const durationMs = Math.round(performance.now() - start);
      return {
        divergenceAnalysis: `Predicted stdout was "${predictedStdout}", but received exitCode=${actualResult.exitCode} with output: "${actualResult.stdout || actualResult.stderr}".`,
        chainOfThought:
          "The tool execution diverged from speculative prediction. Examining error stack trace: unexpected assertion failure on line 42. Formulating patch to align return types.",
        recoveredAction: {
          thought: "Fixing assertion failure by updating expected schema definition.",
          nextTool: {
            id: `call_rec_${Date.now()}`,
            name: "write_file",
            args: { path: "src/schema.ts", patch: "Fixing return type mismatch" },
          },
        },
        durationMs,
      };
    }

    try {
      const prompt = `You are System 2 deliberative reasoning engine running on AMD ROCm.
Task: ${taskDescription}
Dispatched Tool: ${toolCall.name}
Actual Result: ${JSON.stringify(actualResult)}
Speculative Prediction: ${predictedStdout}

Analyze the discrepancy, provide chain of thought, and output the corrected next action in JSON:
{
  "divergenceAnalysis": string,
  "chainOfThought": string,
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
          temperature: 0.6,
          max_tokens: 800,
        }),
      });

      const data = (await res.json()) as any;
      const content = data.choices?.[0]?.message?.content || "{}";
      const parsed = JSON.parse(content.match(/\{[\s\S]*\}/)?.[0] || "{}");
      const durationMs = Math.round(performance.now() - start);

      return {
        divergenceAnalysis: parsed.divergenceAnalysis || "Divergence detected",
        chainOfThought: parsed.chainOfThought || "Analyzed tool output and error conditions.",
        recoveredAction: {
          thought: parsed.nextThought || "Applying corrective action.",
          nextTool: {
            id: `call_rec_${Date.now()}`,
            name: parsed.nextToolName || "bash",
            args: parsed.nextToolArgs || {},
          },
        },
        durationMs,
      };
    } catch {
      const durationMs = Math.round(performance.now() - start);
      return {
        divergenceAnalysis: "Fallback analysis on unexpected tool failure.",
        chainOfThought: "Re-evaluating plan based on non-zero exit code.",
        recoveredAction: {
          thought: "Retrying command with debug flags.",
          nextTool: {
            id: `call_rec_${Date.now()}`,
            name: "bash",
            args: { command: `${toolCall.args.command || ""} --verbose` },
          },
        },
        durationMs,
      };
    }
  }
}
