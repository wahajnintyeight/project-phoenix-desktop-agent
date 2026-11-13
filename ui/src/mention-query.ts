export function mentionQuery(value: string, caret: number, selectionEnd: number): { start: number; query: string } | null {
  if (caret !== selectionEnd) return null;
  const before = value.slice(0, caret);
  const masked = before.replace(/@(?:Task|File|Folder|MCP):"(?:\\.|[^"\\])*"/g, (reference) => " ".repeat(reference.length));
  const match = /(?:^|[^\p{L}\p{N}_@])@([^@\n]{0,100})$/u.exec(masked);
  if (!match) return null;
  const start = caret - match[1]!.length - 1;
  return { start, query: before.slice(start + 1) };
}
