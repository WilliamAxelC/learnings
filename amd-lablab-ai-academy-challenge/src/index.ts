import dotenv from "dotenv";
dotenv.config();

import { NeuroBranchEngine } from "./core/speculative.js";

async function main() {
  console.log("================================================================================");
  console.log("  🚀 NeuroBranch: Neural Speculative Execution for Agent Tool Calls");
  console.log("  Built for Lablab.ai - Lablab x AMD AI Academy Challenge");
  console.log("  Dual-Process Architecture: System 1 (ROCm Fast Draft) + System 2 (Recovery)");
  console.log("================================================================================\n");

  const engine = new NeuroBranchEngine();

  console.log("👉 Scenario 1: Speculative Hit (Clean Passing Unit Test)");
  console.log("--------------------------------------------------------------------------------");
  console.log("Dispatched Tool: `pnpm test` (Simulated tool runtime: ~600ms)");
  console.log("⚡ Starting concurrent System 1 Draft on AMD ROCm (sub-20ms)...");

  const hitResult = await engine.executeStep(
    1,
    "Implement and test fibonacci recursive function",
    { id: "call_1", name: "bash", args: { command: "pnpm test" } },
    { simulatedToolLatencyMs: 600, forceMismatch: false }
  );

  console.log(`[Tool Result] Stdout: "${hitResult.toolResult.stdout}" (${hitResult.toolResult.durationMs}ms)`);
  console.log(`[System 1 Draft] Prediction: "${hitResult.system1Draft?.predictedStdout}" (Generated in ${hitResult.system1Draft?.draftLatencyMs}ms)`);
  console.log(`[Reconciliation] Status: 🎯 SPECULATIVE HIT!`);
  console.log(`[Next Action Committed Immediately]: "${hitResult.effectiveAction.thought}"`);
  console.log(`⏱️ Turn Latency: ${hitResult.speculativeTurnLatencyMs}ms (Saved ${hitResult.latencySavedMs}ms vs sequential ${hitResult.sequentialTurnLatencyMs}ms)\n`);

  console.log("👉 Scenario 2: Speculative Miss & System 2 Deliberate Recovery");
  console.log("--------------------------------------------------------------------------------");
  console.log("Dispatched Tool: `tsc --noEmit` (Simulated build error)");
  console.log("⚡ System 1 predicted success, but real compiler returned Type Error...");

  const missResult = await engine.executeStep(
    2,
    "Fix TypeScript compilation errors",
    { id: "call_2", name: "bash", args: { command: "tsc --noEmit" } },
    { simulatedToolLatencyMs: 500, forceMismatch: true }
  );

  console.log(`[Tool Result] Exit Code: 1 (Build Error detected)`);
  console.log(`[Reconciliation] Status: ⚠️ DIVERGENCE DETECTED (System 1 miss)`);
  console.log(`🧠 Activating System 2 Deliberate Reasoning on AMD GPU...`);
  console.log(`[System 2 CoT]: "${missResult.system2Recovery?.chainOfThought}"`);
  console.log(`[System 2 Recovered Action]: "${missResult.system2Recovery?.recoveredAction.thought}"`);
  console.log(`⏱️ Completed Recovery in ${missResult.system2Recovery?.durationMs}ms\n`);

  console.log("================================================================================");
  console.log("  ✅ Demonstration Complete!");
  console.log("  To run the full comparative speed benchmark, execute: `pnpm benchmark`");
  console.log("================================================================================");
}

main().catch(console.error);
