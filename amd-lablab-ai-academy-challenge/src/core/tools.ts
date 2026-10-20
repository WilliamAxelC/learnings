import { exec } from "child_process";
import { promisify } from "util";

const execAsync = promisify(exec);

export interface ToolCall {
  id: string;
  name: string;
  args: Record<string, any>;
}

export interface ToolResult {
  toolCallId: string;
  toolName: string;
  stdout: string;
  stderr: string;
  exitCode: number;
  durationMs: number;
}

export class ToolExecutor {
  /**
   * Executes a tool either through a real subprocess or a controlled benchmark task
   */
  async execute(toolCall: ToolCall, options?: { simulatedLatencyMs?: number }): Promise<ToolResult> {
    const startTime = performance.now();

    if (options?.simulatedLatencyMs) {
      await new Promise((resolve) => setTimeout(resolve, options.simulatedLatencyMs));
    }

    if (toolCall.name === "bash" || toolCall.name === "run_command") {
      const command = toolCall.args.command || "echo 'done'";
      try {
        const { stdout, stderr } = await execAsync(command, { timeout: 15000 });
        const durationMs = Math.round(performance.now() - startTime);
        return {
          toolCallId: toolCall.id,
          toolName: toolCall.name,
          stdout: stdout.trim(),
          stderr: stderr.trim(),
          exitCode: 0,
          durationMs,
        };
      } catch (err: any) {
        const durationMs = Math.round(performance.now() - startTime);
        return {
          toolCallId: toolCall.id,
          toolName: toolCall.name,
          stdout: err.stdout?.trim() || "",
          stderr: err.stderr?.trim() || err.message,
          exitCode: err.code || 1,
          durationMs,
        };
      }
    }

    // Default mock execution
    const durationMs = Math.round(performance.now() - startTime);
    return {
      toolCallId: toolCall.id,
      toolName: toolCall.name,
      stdout: JSON.stringify({ success: true, tool: toolCall.name }),
      stderr: "",
      exitCode: 0,
      durationMs,
    };
  }
}
