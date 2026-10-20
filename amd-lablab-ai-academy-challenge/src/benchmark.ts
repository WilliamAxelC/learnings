import dotenv from "dotenv";
dotenv.config();

import { NeuroBranchEngine } from "./core/speculative.js";
import { ToolCall } from "./core/tools.js";

async function runBenchmark() {
  console.log("\n================================================================================");
  console.log("  ⚡ NeuroBranch: AMD ROCm Dual-Process Speculative Execution Benchmark");
  console.log("  Hardware Target: AMD Instinct™ GPU (vLLM ROCm Runtime)");
  console.log("================================================================================\n");

  const engine = new NeuroBranchEngine();

  const benchmarkSteps: Array<{ tool: ToolCall; simulatedDurationMs: number; forceMismatch?: boolean }> = [
    {
      tool: { id: "step_1", name: "bash", args: { command: "pnpm lint" } },
      simulatedDurationMs: 350,
      forceMismatch: false,
    },
    {
      tool: { id: "step_2", name: "bash", args: { command: "tsc --noEmit" } },
      simulatedDurationMs: 600,
      forceMismatch: false,
    },
    {
      tool: { id: "step_3", name: "bash", args: { command: "pnpm test" } },
      simulatedDurationMs: 800,
      forceMismatch: false,
    },
    {
      tool: { id: "step_4", name: "bash", args: { command: "pnpm build" } },
      simulatedDurationMs: 1200,
      forceMismatch: true, // Deliberate failure/divergence to test System 2 recovery
    },
    {
      tool: { id: "step_5", name: "bash", args: { command: "git status" } },
      simulatedDurationMs: 150,
      forceMismatch: false,
    },
  ];

  let totalSequentialMs = 0;
  let totalSpeculativeMs = 0;
  let totalHits = 0;
  let totalMisses = 0;

  console.log(
    `| Step | Dispatched Tool      | Tool Wait | Status | Speculative Latency | Sequential Latency | Time Saved |`
  );
  console.log(
    `| :--- | :------------------- | :-------- | :----- | :------------------ | :----------------- | :--------- |`
  );

  for (let i = 0; i < benchmarkSteps.length; i++) {
    const item = benchmarkSteps[i];
    const result = await engine.executeStep(
      i + 1,
      "Refactor and verify authentication token handler",
      item.tool,
      {
        simulatedToolLatencyMs: item.simulatedDurationMs,
        forceMismatch: item.forceMismatch,
      }
    );

    totalSequentialMs += result.sequentialTurnLatencyMs;
    totalSpeculativeMs += result.speculativeTurnLatencyMs;

    if (result.speculativeHit) {
      totalHits++;
    } else {
      totalMisses++;
    }

    const statusBadge = result.speculativeHit ? "🎯 HIT " : "⚠️ MISS";
    const toolName = (item.tool.args.command || item.tool.name).padEnd(20, " ");

    console.log(
      `| #${i + 1}   | \`${toolName}\` | ${item.simulatedDurationMs.toString().padStart(4, " ")} ms  | ${statusBadge} | ${result.speculativeTurnLatencyMs.toString().padStart(9, " ")} ms    | ${result.sequentialTurnLatencyMs.toString().padStart(9, " ")} ms       | +${result.latencySavedMs.toString().padStart(4, " ")} ms   |`
    );
  }

  const speedup = (totalSequentialMs / totalSpeculativeMs).toFixed(2);
  const totalSavedMs = totalSequentialMs - totalSpeculativeMs;
  const hitRate = Math.round((totalHits / benchmarkSteps.length) * 100);

  console.log("\n================================================================================");
  console.log("  📊 Benchmark Summary Results");
  console.log("================================================================================");
  console.log(`  * Total Evaluated Steps:         ${benchmarkSteps.length}`);
  console.log(`  * Speculative Hit Rate:          ${hitRate}% (${totalHits} Hits, ${totalMisses} Misses)`);
  console.log(`  * Sequential Execution Time:     ${totalSequentialMs} ms`);
  console.log(`  * NeuroBranch Execution Time:    ${totalSpeculativeMs} ms`);
  console.log(`  * Total Latency Eliminated:      ${totalSavedMs} ms`);
  console.log(`  * Effective Speedup:             ${speedup}x faster`);
  console.log("================================================================================\n");
}

runBenchmark().catch(console.error);
