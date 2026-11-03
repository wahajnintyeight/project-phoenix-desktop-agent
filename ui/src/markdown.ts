// A small, safe Markdown renderer for agent replies. It builds DOM nodes with
// createElement and textContent only: model output is untrusted, so nothing is ever parsed
// as HTML, and links are shown as text (their address in muted type), never as clickable
// anchors. Supported: headings, paragraphs (single newlines kept), bold, italic, strike,
// inline code, fenced code with a copy button, bulleted, numbered and nested lists,
// quotes, rules and tables.
//
// Streaming: a reply is re-rendered as it grows. The text is split into blocks at blank
// lines, and blocks before the one still being written keep their DOM.

type Inline = Node;

const el = <K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string, text?: string): HTMLElementTagNameMap[K] => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
};

// ── Inline ────────────────────────────────────────────────────────────────

const INLINE = /(`+)([\s\S]*?[^`])\1(?!`)|\*\*([^\s*](?:[\s\S]*?[^\s*])?)\*\*|__([^\s_](?:[\s\S]*?[^\s_])?)__|~~([^\s~](?:[\s\S]*?[^\s~])?)~~|\*([^\s*](?:[^*\n]*?[^\s*])?)\*|(?<![\w])_([^\s_](?:[^_\n]*?[^\s_])?)_(?![\w])|\[([^\]\n]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g;

function hostOf(url: string): string {
  try {
    const u = new URL(url);
    return u.protocol === "http:" || u.protocol === "https:" ? u.host : "";
  } catch {
    return "";
  }
}

/** Inline formatting inside one run of text. Lines are joined with <br>. */
export function inline(text: string, into: HTMLElement): void {
  const lines = text.split("\n");
  lines.forEach((line, i) => {
    if (i > 0) into.append(el("br"));
    into.append(...inlineLine(line));
  });
}

function inlineLine(s: string): Inline[] {
  // A pathological line is shown as it is rather than scanned for formatting.
  if (s.length > 4000) return [document.createTextNode(s)];
  const out: Inline[] = [];
  let last = 0;
  // matchAll works on its own copy of the regex: the recursive calls below must not disturb
  // this loop's position.
  for (const m of s.matchAll(INLINE)) {
    const at = m.index ?? 0;
    if (at > last) out.push(document.createTextNode(s.slice(last, at)));
    last = at + m[0].length;
    if (m[2] !== undefined) {
      out.push(el("code", "md-code", m[2].replace(/^ (.*) $/, "$1")));
    } else if (m[3] !== undefined || m[4] !== undefined) {
      const b = el("strong");
      b.append(...inlineLine(m[3] ?? m[4] ?? ""));
      out.push(b);
    } else if (m[5] !== undefined) {
      const d = el("del");
      d.append(...inlineLine(m[5]));
      out.push(d);
    } else if (m[6] !== undefined || m[7] !== undefined) {
      const i = el("em");
      i.append(...inlineLine(m[6] ?? m[7] ?? ""));
      out.push(i);
    } else if (m[8] !== undefined && m[9] !== undefined) {
      // A link is text plus where it goes; the user decides what to do with the address.
      out.push(document.createTextNode(m[8]));
      const host = hostOf(m[9]);
      if (host) out.push(el("span", "md-link-host", ` (${host})`));
    }
  }
  if (last < s.length) out.push(document.createTextNode(s.slice(last)));
  return out;
}

// ── Blocks ────────────────────────────────────────────────────────────────

const FENCE = /^ {0,3}(`{3,}|~{3,})\s*([\w+#.-]*)\s*$/;
const HEADING = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const RULE = /^ {0,3}([-*_])(?:\s*\1){2,}\s*$/;
const QUOTE = /^ {0,3}>\s?(.*)$/;
const ITEM = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/;
const TABLE_SEP = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;

const isBlank = (l: string): boolean => l.trim() === "";

function splitRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
  return s.split(/(?<!\\)\|/).map((c) => c.trim().replace(/\\\|/g, "|"));
}

function startsBlock(line: string, next: string | undefined): boolean {
  return FENCE.test(line) || HEADING.test(line) || RULE.test(line) || QUOTE.test(line) || ITEM.test(line) ||
    (line.includes("|") && next !== undefined && TABLE_SEP.test(next));
}

function codeBlock(code: string, lang: string): HTMLElement {
  const wrap = el("div", "md-pre");
  const head = el("div", "md-pre-head");
  head.append(el("span", "md-pre-lang", lang));
  const copy = el("button", "md-copy", "Copy");
  copy.type = "button";
  copy.addEventListener("click", () => {
    void navigator.clipboard.writeText(code).then(
      () => { copy.textContent = "Copied"; setTimeout(() => (copy.textContent = "Copy"), 1500); },
      () => { copy.textContent = "Copy failed"; setTimeout(() => (copy.textContent = "Copy"), 1500); },
    );
  }, { passive: true });
  head.append(copy);
  const pre = el("pre");
  pre.append(el("code", undefined, code));
  pre.tabIndex = 0; // scrollable regions must be reachable by keyboard
  wrap.append(head, pre);
  return wrap;
}

/** Renders one list starting at lines[i]; returns the element and the next line index. */
function list(lines: string[], i: number): [HTMLElement, number] {
  const first = ITEM.exec(lines[i] ?? "");
  const baseIndent = first?.[1]?.length ?? 0;
  const ordered = /\d/.test(first?.[2] ?? "");
  const root = el(ordered ? "ol" : "ul", "md-list");
  if (ordered) {
    const n = parseInt(first?.[2] ?? "1", 10);
    if (n !== 1) root.setAttribute("start", String(n));
  }
  let li: HTMLLIElement | null = null;
  while (i < lines.length) {
    const line = lines[i] ?? "";
    const m = ITEM.exec(line);
    if (m) {
      const indent = m[1]?.length ?? 0;
      if (indent < baseIndent) break;
      if (indent >= baseIndent + 2 && li) {
        const [nested, next] = list(lines, i);
        li.append(nested);
        i = next;
        continue;
      }
      if (/\d/.test(m[2] ?? "") !== ordered) break;
      li = el("li");
      const text = m[3] ?? "";
      const task = /^\[([ xX])\]\s+(.*)$/.exec(text);
      if (task) {
        const box = el("input");
        box.type = "checkbox";
        box.disabled = true;
        box.checked = task[1] !== " ";
        li.append(box, document.createTextNode(" "));
        inline(task[2] ?? "", li);
      } else {
        inline(text, li);
      }
      root.append(li);
      i++;
    } else if (li && !isBlank(line) && /^\s{2,}\S/.test(line) && !startsBlock(line.trim(), undefined)) {
      li.append(el("br"));
      inline(line.trim(), li); // a wrapped line of the same item
      i++;
    } else {
      break;
    }
  }
  return [root, i];
}

/** Renders a run of Markdown (no blank-line splitting is assumed) into nodes. */
export function blocks(src: string): HTMLElement[] {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const out: HTMLElement[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? "";
    if (isBlank(line)) { i++; continue; }

    const fence = FENCE.exec(line);
    if (fence) {
      const mark = fence[1] ?? "```";
      const body: string[] = [];
      i++;
      while (i < lines.length && !(lines[i] ?? "").trim().startsWith(mark)) body.push(lines[i++] ?? "");
      if (i < lines.length) i++; // the closing fence; a reply still streaming has none yet
      out.push(codeBlock(body.join("\n"), fence[2] ?? ""));
      continue;
    }
    const h = HEADING.exec(line);
    if (h) {
      const level = Math.min(h[1]?.length ?? 1, 4);
      const e = el(`h${level + 1}` as "h2" | "h3" | "h4" | "h5", "md-h");
      inline(h[2] ?? "", e);
      out.push(e);
      i++;
      continue;
    }
    if (RULE.test(line)) { out.push(el("hr", "md-hr")); i++; continue; }
    if (QUOTE.test(line)) {
      const body: string[] = [];
      while (i < lines.length && QUOTE.test(lines[i] ?? "")) body.push(QUOTE.exec(lines[i++] ?? "")?.[1] ?? "");
      const q = el("blockquote", "md-quote");
      q.append(...blocks(body.join("\n")));
      out.push(q);
      continue;
    }
    if (ITEM.test(line)) {
      const [l, next] = list(lines, i);
      out.push(l);
      i = next;
      continue;
    }
    if (line.includes("|") && i + 1 < lines.length && TABLE_SEP.test(lines[i + 1] ?? "")) {
      const head = splitRow(line);
      const aligns = splitRow(lines[i + 1] ?? "").map((c) => (c.startsWith(":") && c.endsWith(":") ? "center" : c.endsWith(":") ? "right" : ""));
      i += 2;
      const table = el("table", "md-table");
      const thead = el("thead");
      const hr = el("tr");
      head.forEach((c, k) => {
        const th = el("th");
        inline(c, th);
        if (aligns[k]) th.style.textAlign = aligns[k] ?? "";
        hr.append(th);
      });
      thead.append(hr);
      const tbody = el("tbody");
      while (i < lines.length && !isBlank(lines[i] ?? "") && (lines[i] ?? "").includes("|")) {
        const tr = el("tr");
        splitRow(lines[i++] ?? "").forEach((c, k) => {
          const td = el("td");
          inline(c, td);
          if (aligns[k]) td.style.textAlign = aligns[k] ?? "";
          tr.append(td);
        });
        tbody.append(tr);
      }
      table.append(thead, tbody);
      const scroll = el("div", "md-table-wrap");
      scroll.append(table);
      out.push(scroll);
      continue;
    }
    // Paragraph: until a blank line or the start of another block.
    const para: string[] = [];
    while (i < lines.length && !isBlank(lines[i] ?? "") && (para.length === 0 || !startsBlock(lines[i] ?? "", lines[i + 1]))) {
      para.push(lines[i++] ?? "");
    }
    const p = el("p", "md-p");
    inline(para.join("\n"), p);
    out.push(p);
  }
  return out;
}

/** Splits at blank lines outside code fences; a list continues across its blank lines. */
function segments(src: string): string[] {
  const out: string[] = [];
  let cur: string[] = [];
  let fence = "";
  const flush = (): void => {
    if (cur.length) out.push(cur.join("\n"));
    cur = [];
  };
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  lines.forEach((line, idx) => {
    const f = FENCE.exec(line);
    if (fence) {
      cur.push(line);
      if (f && (f[1] ?? "").startsWith(fence) && !f[2]) fence = "";
      return;
    }
    if (f) {
      flush();
      fence = (f[1] ?? "```").slice(0, 3);
      cur.push(line);
      return;
    }
    if (isBlank(line)) {
      // Keep going when the next non-blank line is a list item and this segment is a list.
      const nextLine = lines.slice(idx + 1).find((l) => !isBlank(l));
      if (cur.length && ITEM.test(cur[0] ?? "") && nextLine !== undefined && ITEM.test(nextLine)) {
        cur.push(line);
        return;
      }
      flush();
      return;
    }
    cur.push(line);
  });
  flush();
  return out;
}

/** A Markdown view that updates cheaply while a reply streams in. */
export class MarkdownView {
  readonly #root: HTMLElement;
  #segs: string[] = [];
  #nodes: HTMLElement[][] = [];

  constructor(root: HTMLElement) {
    this.#root = root;
  }

  set(src: string): void {
    const segs = segments(src);
    // The last segment is the one still being written; every earlier one that is unchanged
    // keeps its DOM.
    let keep = 0;
    while (keep < segs.length && keep < this.#segs.length - 1 && segs[keep] === this.#segs[keep]) keep++;
    for (const group of this.#nodes.slice(keep)) for (const n of group) n.remove();
    this.#nodes.length = keep;
    for (let i = keep; i < segs.length; i++) {
      const nodes = blocks(segs[i] ?? "");
      this.#root.append(...nodes);
      this.#nodes.push(nodes);
    }
    this.#segs = segs;
  }
}
