export interface ChoiceSet {
  question: string;
  options: string[];
  kind: "question" | "action";
}

const MAX_GROUPS = 4;
const MAX_OPTIONS = 8;
const MAX_LABEL = 120;
const COMPLETE = /(?:^|\n)[ \t]*```(choices|actions|suggestions)[ \t]*\n([\s\S]*?)\n[ \t]*```[ \t]*(?=\n|$)/g;
const OPEN = /(?:^|\n)[ \t]*```(?:choices|actions|suggestions)[\s\S]*$/;

/** Extract complete question menus and hide a menu that is still streaming. */
export function splitChoices(raw: string): { body: string; choices: ChoiceSet[] } {
  const choices: ChoiceSet[] = [];
  let body = "";
  let last = 0;
  for (const match of raw.matchAll(COMPLETE)) {
    const at = match.index ?? 0;
    body += raw.slice(last, at);
    last = at + match[0].length;
    const choice = match[1] === "suggestions" ? null : parse(match[2] ?? "", match[1] === "actions" ? "action" : "question");
    if (choice && choices.length < MAX_GROUPS) choices.push(choice);
    else if (match[1] !== "suggestions") body += match[0];
  }
  const tail = raw.slice(last);
  const open = OPEN.exec(tail);
  body += open ? tail.slice(0, open.index) : tail;
  return { body: body.trim(), choices };
}

function parse(block: string, kind: ChoiceSet["kind"]): ChoiceSet | null {
  const options: string[] = [];
  let question = "";
  for (const line of block.split("\n")) {
    const text = line.trim();
    if (!text) continue;
    const item = /^(?:[-*•]|\d+[.)])\s+(.*)$/.exec(text);
    if (item) {
      const label = (item[1] ?? "").replace(/\*\*/g, "").trim().slice(0, MAX_LABEL);
      if (label && options.length < MAX_OPTIONS && !options.includes(label)) options.push(label);
    } else if (!question && options.length === 0) {
      question = text.replace(/^\?\s*/, "").slice(0, 160);
    }
  }
  return question && options.length ? { question, options, kind } : null;
}
