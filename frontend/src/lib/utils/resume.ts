/** Agent types that support CLI session resumption. */
const RESUME_AGENTS: Record<string, (sessionId: string) => string> = {
  claude: (id) => `claude --resume ${shellQuote(id)}`,
  codex: (id) => `codex resume ${shellQuote(id)}`,
  copilot: (id) => `copilot --resume=${shellQuote(id)}`,
  cursor: (id) => `cursor agent --resume ${shellQuote(id)}`,
  gemini: (id) => `gemini --resume ${shellQuote(id)}`,
  opencode: (id) => `opencode --session ${shellQuote(id)}`,
  amp: (id) => `amp --resume ${shellQuote(id)}`,
};

const DESKTOP_RESUME_AGENTS = new Set(["claude", "claude-cowork"]);

/**
 * Agents whose resume commands require server-resolved parameters
 * (e.g. --workspace, cwd) that the client cannot compute locally.
 * buildResumeCommand returns null for these agents so callers
 * don't produce incomplete fallback commands.
 */
const SERVER_ONLY_RESUME = new Set(["cursor"]);

/** Flags available for Claude Code resume. */
export interface ClaudeResumeFlags {
  skipPermissions?: boolean;
  forkSession?: boolean;
  print?: boolean;
}

/** Minimal shape of a backend resume response used for clipboard copy. */
export interface ResumeCommandResponse {
  command: string;
  cwd?: string;
}

/**
 * POSIX-safe shell quoting using single quotes.
 * Any embedded single quotes are escaped as '"'"'.
 * Skips quoting for IDs that are purely alphanumeric + hyphens.
 */
function shellQuote(s: string): string {
  if (/^[a-zA-Z0-9_][\w-]*$/.test(s)) return s;
  return "'" + s.replace(/'/g, "'\"'\"'") + "'";
}

function commandWithCwd(cmd: string, cwd?: string): string {
  if (!cwd) return cmd;
  return `cd ${shellQuote(cwd)} && ${cmd}`;
}

/**
 * Strip the agent-type prefix from a compound session ID, but only
 * when the prefix matches a known agent. Raw IDs that happen to
 * contain ":" are left untouched.
 */
export function stripIdPrefix(id: string, agent?: string): string {
  if (agent) {
    const prefix = agent + ":";
    if (id.startsWith(prefix)) return id.slice(prefix.length);
  }
  return id;
}

/**
 * Returns true if the given agent supports session resumption via
 * either a terminal command or Claude Desktop.
 */
export function supportsResume(agent: string): boolean {
  return supportsTerminalResume(agent) || supportsDesktopResume(agent);
}

/** Returns true if the given agent can resume via Claude Desktop. */
export function supportsDesktopResume(agent: string): boolean {
  return DESKTOP_RESUME_AGENTS.has(agent);
}

/**
 * Returns true if the given agent supports launching a terminal
 * resume flow, even when the exact command must come from the server.
 */
export function supportsTerminalResume(agent: string): boolean {
  return Object.hasOwn(RESUME_AGENTS, agent);
}

/**
 * Build a CLI command to resume the given session in a terminal.
 *
 * @param agent - The agent type (e.g. "claude", "codex", "cursor")
 * @param sessionId - The session ID (may include agent prefix)
 * @param flags - Optional Claude-specific resume flags
 * @returns The shell command string, or null if the agent is not supported
 */
export function buildResumeCommand(
  agent: string,
  sessionId: string,
  flags?: ClaudeResumeFlags,
): string | null {
  if (SERVER_ONLY_RESUME.has(agent)) return null;
  const builder = RESUME_AGENTS[agent];
  if (!builder) return null;

  const rawId = stripIdPrefix(sessionId, agent);
  let cmd = builder(rawId);

  if (agent === "claude" && flags) {
    if (flags.skipPermissions) cmd += " --dangerously-skip-permissions";
    if (flags.forkSession) cmd += " --fork-session";
    if (flags.print) cmd += " --print";
  }

  return cmd;
}

/**
 * Prefer a backend-built command when available, otherwise fall back
 * to the local CLI builder for agents whose resume command can be
 * derived client-side.
 */
export function resolveResumeCommand(
  agent: string,
  sessionId: string,
  response: ResumeCommandResponse | null | undefined,
): string | null {
  return (
    formatResumeResponseCommand(agent, response) ||
    buildResumeCommand(agent, sessionId)
  );
}

/**
 * Format a backend-built resume response for clipboard copy.
 *
 * Cursor keeps `command` and `cwd` separate in the API so callers can
 * choose whether to apply the cwd directly. Clipboard copy needs a
 * runnable one-liner, so rebuild it here only for Cursor.
 */
export function formatResumeResponseCommand(
  agent: string,
  response: ResumeCommandResponse | null | undefined,
): string | null {
  if (!response?.command) return null;
  if (agent !== "cursor") return response.command;
  return commandWithCwd(response.command, response.cwd);
}
