// One registry for the chat's slash menu, filtering, aliases and execution targets.
export const SlashCommandId = {
  Help: "help",
  Clear: "clear",
  Stop: "stop",
  Share: "share",
  Settings: "settings",
  Model: "model",
  Summarise: "summarise",
  KeyPoints: "keypoints",
  Explain: "explain",
  Next: "next",
  TLDR: "tldr",
  Plan: "plan",
  FollowUp: "followup",
  FactCheck: "factcheck",
  CoverLetter: "coverletter",
  MCPs: "mcps",
} as const;

export type SlashCommandId = (typeof SlashCommandId)[keyof typeof SlashCommandId];

export type SlashCommandAction =
  | { type: "help" }
  | { type: "clear" }
  | { type: "stop" }
  | { type: "share" }
  | { type: "settings" }
  | { type: "model" }
  | { type: "list-mcps" }
  | { type: "quick-action"; id: string };

export interface SlashCommandDefinition {
  id: SlashCommandId;
  description: string;
  action: SlashCommandAction;
  aliases?: readonly string[];
}

export const SLASH_COMMANDS: readonly SlashCommandDefinition[] = [
  { id: SlashCommandId.Help, aliases: ["commands"], description: "Show all available commands", action: { type: "help" } },
  { id: SlashCommandId.Clear, aliases: ["new"], description: "Start a fresh conversation", action: { type: "clear" } },
  { id: SlashCommandId.Stop, description: "Stop the current reply", action: { type: "stop" } },
  { id: SlashCommandId.Share, description: "Toggle sharing the open page", action: { type: "share" } },
  { id: SlashCommandId.Settings, description: "Open Phoenix settings", action: { type: "settings" } },
  { id: SlashCommandId.Model, description: "Choose a provider or model", action: { type: "model" } },
  { id: SlashCommandId.Summarise, aliases: ["summary"], description: "Summarise the open page", action: { type: "quick-action", id: "summarise" } },
  { id: SlashCommandId.KeyPoints, aliases: ["key-points"], description: "List the page's key points", action: { type: "quick-action", id: "keypoints" } },
  { id: SlashCommandId.Explain, aliases: ["simplify"], description: "Explain the page in plain language", action: { type: "quick-action", id: "simplify" } },
  { id: SlashCommandId.Next, aliases: ["nextstep"], description: "Suggest a sensible next step", action: { type: "quick-action", id: "nextstep" } },
  { id: SlashCommandId.TLDR, description: "Get a three-sentence TL;DR", action: { type: "quick-action", id: "tldr" } },
  { id: SlashCommandId.Plan, description: "Plan a focused job search", action: { type: "quick-action", id: "plan" } },
  { id: SlashCommandId.FollowUp, aliases: ["follow-up"], description: "Draft a polite follow-up email", action: { type: "quick-action", id: "followup" } },
  { id: SlashCommandId.FactCheck, aliases: ["fact-check"], description: "Review an article's factual claims", action: { type: "quick-action", id: "factcheck" } },
  { id: SlashCommandId.CoverLetter, aliases: ["cover-letter"], description: "Draft a cover letter for a job page", action: { type: "quick-action", id: "coverletter" } },
  { id: SlashCommandId.MCPs, aliases: ["mcp"], description: "List enabled MCPs that passed their last test", action: { type: "list-mcps" } },
];

export function searchSlashCommands(input: string): SlashCommandDefinition[] {
  if (!input.startsWith("/")) return [];
  const query = input.slice(1).split(/\s/, 1)[0]?.toLowerCase() ?? "";
  return SLASH_COMMANDS.filter((command) => command.id.startsWith(query) || command.aliases?.some((alias) => alias.startsWith(query)));
}

export function resolveSlashCommand(input: string): SlashCommandDefinition | undefined {
  if (!input.startsWith("/")) return undefined;
  const name = input.slice(1).trim().split(/\s+/, 1)[0]?.toLowerCase();
  if (!name) return undefined;
  return SLASH_COMMANDS.find((command) => command.id === name || command.aliases?.includes(name));
}
