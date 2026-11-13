export function mentionQuery(value: string, caret: number, selectionEnd: number): { start: number; query: string } | null {
  if (caret !== selectionEnd) return null;
  const match = /(?:^|[^\p{L}\p{N}_@])@([^@\n]{0,100})$/u.exec(value.slice(0, caret));
  return match ? { start: caret - match[1]!.length - 1, query: match[1]! } : null;
}
