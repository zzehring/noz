/**
 * noz gate for pi / oh-my-pi.
 *
 * Claude Code gates tool calls with an out-of-process hook: a JSON payload on
 * stdin, a verdict in the exit code. pi has no such mechanism — its extension
 * point is in-process TypeScript — so this extension is the adapter. It hands
 * each tool call to `noz gate` and turns the verdict back into pi's own
 * blocking contract.
 *
 * The mapping gains something Claude's hook cannot express. noz policies have
 * three verdicts; an exit code only carries two usefully, so PAUSE under Claude
 * is just a block. Here PAUSE becomes ctx.ui.confirm(), which is what PAUSE
 * always meant: ask the human.
 *
 *   ALLOW (exit 0)  -> proceed
 *   DENY  (exit 2)  -> block, and terminate the turn
 *   PAUSE (exit 3)  -> ask; proceed if approved, block if not
 *   anything else   -> block, because a gate that cannot answer must not pass
 *
 * Deliberately no package imports, so the same file works under upstream `pi`
 * (@earendil-works/pi-coding-agent) and under `omp`
 * (@oh-my-pi/pi-coding-agent) without being pinned to either package name.
 *
 * KNOWN LIMIT, read before trusting this: only the tools noz models are gated —
 * bash, read, write, edit. pi and omp ship many more (LSP, find, web, custom
 * tools from other extensions), and those pass through ungated. The extension
 * reports what it let through at session start so the gap is visible rather
 * than silent. A gate that quietly covers a quarter of the tool surface while
 * reading as protection is the failure noz keeps trying not to ship.
 */

type Block = { block: true; reason?: string; terminate?: boolean };

// pi tool names mapped to the --tool value noz evaluates them as. ast_edit is
// mapped to edit on purpose: it modifies files, so it must face the same write
// containment as a plain edit rather than slip past it.
const GATED: Record<string, string> = {
  bash: "bash",
  read: "read",
  write: "write",
  edit: "edit",
  multiedit: "edit",
  apply_patch: "edit",
  ast_edit: "edit",
  glob: "glob",
  grep: "grep",
};

// Tools with no file-write, code-execution, or network capability. Passed
// through because gating them buys nothing and blocking them would make the
// agent's ordinary workflow unusable, so users would turn the gate off.
const PASS_THROUGH = new Set(["todo", "wait", "lsp", "report_issue", "debug"]);

// Everything not in GATED or PASS_THROUGH is denied by default — including
// eval (arbitrary code), web_search (network egress), and task (an unbounded
// subagent). noz's gate does not model these, and passing an unmodeled tool
// through with a warning is the default-allow failure the policies exist to
// avoid: under omp, "unknown" includes eval. A user who has decided a tool is
// safe can allow it explicitly with NOZ_GATE_ALLOW=tool1,tool2.
function userAllowed(): Set<string> {
  const raw = process.env.NOZ_GATE_ALLOW ?? "";
  return new Set(raw.split(",").map((t) => t.trim()).filter(Boolean));
}

/** Build the JSON noz's gate reads on stdin. */
function gateInput(tool: string, input: any): string | null {
  if (tool === "bash") {
    const command = input?.command;
    if (typeof command !== "string" || command === "") return null;
    return JSON.stringify({ command });
  }
  // noz's extractPath accepts file_path, path, or pattern. Cover each so the
  // read/edit/glob/grep tools all present a path the gate can evaluate.
  const path = input?.path ?? input?.file_path ?? input?.pattern;
  if (typeof path !== "string" || path === "") return null;
  return JSON.stringify({ file_path: path });
}

type GateResult = { code: number; stderr: string };

/** Run `noz gate` with the payload on stdin and collect its verdict. */
async function runGate(tool: string, payload: string, cwd: string): Promise<GateResult> {
  const { spawn } = await import("node:child_process");

  const args = ["gate", "--tool", tool];
  const policy = process.env.NOZ_POLICY;
  if (policy) args.push("--policy", policy);

  return new Promise<GateResult>((resolve) => {
    const child = spawn(process.env.NOZ_BIN || "noz", args, {
      cwd,
      // NOZ_PROJECT_DIR tells the gate where the session is rooted, which is
      // what its write-containment rules compare against. Without it the gate
      // falls back to cwd, and a cwd of / or $HOME makes containment vacuous.
      env: { ...process.env, NOZ_PROJECT_DIR: cwd },
      stdio: ["pipe", "ignore", "pipe"],
    });

    let stderr = "";
    child.stderr?.on("data", (d: Buffer) => {
      stderr += d.toString();
    });
    // A gate that cannot run must not pass the call. 126/127 are the shell's
    // "not executable"/"not found", which is the common misconfiguration.
    child.on("error", () => resolve({ code: 127, stderr: "noz not found on PATH" }));
    child.on("close", (code: number | null) => resolve({ code: code ?? 1, stderr }));

    child.stdin?.end(payload);
  });
}

/** Strip noz's "noz: VERDICT " prefix so the reason reads cleanly in the TUI. */
function reasonFrom(stderr: string, fallback: string): string {
  const line = stderr.split("\n").find((l) => l.includes("(rule:")) ?? "";
  return line.replace(/^noz:\s*(DENY|PAUSE)\s*/, "").trim() || fallback;
}

export default function (pi: any) {
  let announced = false;
  const ungated = new Set<string>();

  pi.on("session_start", async (_event: any, ctx: any) => {
    if (announced) return;
    announced = true;
    const policy = process.env.NOZ_POLICY || "noz's default policy";
    ctx.ui?.notify?.(
      `noz gate active (${policy}) — gating file and shell tools; ` +
        `code-exec, network, and subagent tools denied by default`,
      "info",
    );
  });

  pi.on("tool_call", async (event: any, ctx: any): Promise<Block | undefined> => {
    const name = String(event?.toolName ?? "").toLowerCase();
    const tool = GATED[name];

    if (!tool) {
      if (PASS_THROUGH.has(name) || userAllowed().has(name)) {
        return;
      }
      // Default-deny: a tool noz cannot evaluate is off, not waved through.
      if (!ungated.has(name) && name) {
        ungated.add(name);
        ctx.ui?.notify?.(`noz gate blocks "${name}" (not modeled; set NOZ_GATE_ALLOW to permit)`, "warn");
      }
      return {
        block: true,
        reason: `noz gate does not model "${name}" — denied by default`,
        terminate: true,
      };
    }

    const payload = gateInput(tool, event?.input);
    if (payload === null) {
      // A gated tool whose input noz cannot read is not evidence of safety.
      return { block: true, reason: `noz gate: unreadable ${name} input`, terminate: true };
    }

    const cwd = process.cwd();
    const { code, stderr } = await runGate(tool, payload, cwd);

    switch (code) {
      case 0:
        return;
      case 2:
        return { block: true, reason: reasonFrom(stderr, "denied by noz policy"), terminate: true };
      case 3: {
        const detail = reasonFrom(stderr, "this call needs approval");
        const ok = await ctx.ui?.confirm?.("noz gate", `${detail}\n\nAllow it?`);
        if (!ok) return { block: true, reason: "declined at the noz gate" };
        return;
      }
      default:
        return {
          block: true,
          reason: `noz gate could not decide (exit ${code}): ${stderr.trim() || "no output"}`,
          terminate: true,
        };
    }
  });
}
