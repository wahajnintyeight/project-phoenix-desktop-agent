// Naming helpers for the MCP form: pure functions, kept apart from the DOM so they can be tested
// (see mcp-names.test.mjs).

export type Kind = "url" | "command" | "json";

export const lines = (s: string): string[] => s.split("\n").map((l) => l.replace(/\r$/, "")).filter((l) => l.trim() !== "");

/** The same rule Go applies: lowercase letters, digits and single hyphens, at most 32. */
export function normalizeName(s: string): string {
  const out = s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  return out.length > 32 ? out.slice(0, 32).replace(/-+$/g, "") : out;
}

/** Splits "npx -y @scope/pkg" into words, keeping quoted text together. */
export function splitCommand(text: string): string[] {
  const out: string[] = [];
  const re = /"([^"]*)"|'([^']*)'|(\S+)/g;
  for (let m = re.exec(text); m; m = re.exec(text)) out.push(m[1] ?? m[2] ?? m[3] ?? "");
  return out;
}

const RUNNERS = new Set(["npx", "bunx", "uvx", "pnpx", "node", "deno", "python", "python3", "py", "pnpm", "yarn", "npm", "dlx", "exec", "run", "bun", "uv"]);

/** A name worth offering for a server, from its address or from the package its command runs. */
export function suggestName(kind: Kind, url: string, commandLine: string): string {
  if (kind === "url") {
    let host = "";
    try { host = new URL(url.trim()).hostname; } catch { return ""; }
    if (!host) return "";
    if (/^[\d.:[\]]+$/.test(host)) return "local-server";
    const labels = host.split(".").filter((l) => l !== "www" && l !== "mcp" && l !== "api");
    const pick = (labels.length > 1 ? labels.slice(0, -1) : labels)[0] ?? "";
    return normalizeName(pick.replace(/^(api|mcp)-/, ""));
  }
  const words = splitCommand(commandLine);
  if (words.length === 0) return "";
  const base = (path: string): string => path.split(/[\\/]/).pop() ?? "";
  const exe = base(words[0] ?? "").replace(/\.(exe|cmd|bat)$/i, "");
  const runner = RUNNERS.has(exe.toLowerCase());
  const target = runner ? words.slice(1).find((w) => !w.startsWith("-") && !RUNNERS.has(w.toLowerCase())) ?? "" : exe;
  const name = base(target).replace(/@[^@]*$/, "").replace(/\.(m?js|ts|py)$/i, "");
  return normalizeName(name.replace(/^(server|mcp)[-_]/i, "").replace(/[-_](server|mcp)$/i, ""));
}
