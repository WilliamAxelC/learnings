import { ToolCall, ToolResult, ToolExecutor } from "./tools.js";
import { System1DraftEngine, SpeculativeDraft } from "./system1.js";
import { System2ReasoningEngine, RecoveryPlan } from "./system2.js";

export interface SpeculativeStepResult {
  stepIndex: number;
  toolCall: ToolCall;
  toolResult: ToolResult;
  speculativeHit: boolean;
  system1Draft?: SpeculativeDraft;
  system2Recovery?: RecoveryPlan;
  effectiveAction: any;
  sequentialTurnLatencyMs: number;
  speculativeTurnLatencyMs: number;
  latencySavedMs: number;
}

export class NeuroBranchEngine {
  private toolExecutor: ToolExecutor;
  private system1: System1DraftEngine;
  private system2: System2ReasoningEngine;

  constructor() {
    this.toolExecutor = new ToolExecutor();
    this.system1 = new System1DraftEngine();
    this.system2 = new System2ReasoningEngine();
  }

  /**
   * Executes an agent step using Neural Speculative Execution
   */
  async executeStep(
    stepIndex: number,
    taskDescription: string,
    toolCall: ToolCall,
    options?: { simulatedToolLatencyMs?: number; forceMismatch?: boolean }
  ): Promise<SpeculativeStepResult> {
    const stepStart = performance.now();

    // 1. Kick off Real Tool and System 1 Draft in parallel!
    const toolPromise = this.toolExecutor.execute(toolCall, {
      simulatedLatencyMs: options?.simulatedToolLatencyMs ?? 400,
    });

    const s1DraftPromise = this.system1.draftNextStep(taskDescription, toolCall, []);

    // Await both concurrent tasks
    const [toolResult, s1Draft] = await Promise.all([toolPromise, s1DraftPromise]);

    const postToolStart = performance.now();

    // 2. Reconcile Tool Result vs Speculative Prediction
    const isExitCodeMatch = toolResult.exitCode === s1Draft.predictedExitCode;
    const isOutputSemanticallyConsistent =
      !options?.forceMismatch &&
      (toolResult.exitCode === 0 ||
        toolResult.stdout.includes(s1Draft.predictedStdout) ||
        s1Draft.predictedStdout.includes(toolResult.stdout));

    const speculativeHit = isExitCodeMatch && isOutputSemanticallyConsistent;

    let effectiveAction: any;
    let system2Recovery: RecoveryPlan | undefined;
    let postToolWaitMs = 0;

    if (speculativeHit) {
      // 🎯 Hit: 0ms wait after tool! System 1 draft is committed immediately.
      effectiveAction = s1Draft.speculativeNextAction;
      postToolWaitMs = Math.round(performance.now() - postToolStart);
    } else {
      // ⚠️ Miss: Divergence detected -> Invoke System 2 deliberate recovery
      system2Recovery = await this.system2.deliberateRecovery(
        taskDescription,
        toolCall,
        toolResult,
        s1Draft.predictedStdout
      );
      effectiveAction = system2Recovery.recoveredAction;
      postToolWaitMs = Math.round(performance.now() - postToolStart);
    }

    const speculativeTurnLatencyMs = Math.round(performance.now() - stepStart);

    // Baseline calculation: Sequential execution (Tool Duration + 450ms standard reasoning LLM turn)
    const sequentialReasoningTime = 450;
    const sequentialTurnLatencyMs = toolResult.durationMs + sequentialReasoningTime;
    const latencySavedMs = Math.max(0, sequentialTurnLatencyMs - speculativeTurnLatencyMs);

    return {
      stepIndex,
      toolCall,
      toolResult,
      speculativeHit,
      system1Draft: s1Draft,
      system2Recovery,
      effectiveAction,
      sequentialTurnLatencyMs,
      speculativeTurnLatencyMs,
      latencySavedMs,
    };
  }
}
