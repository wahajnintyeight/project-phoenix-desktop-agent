// One icon set for the UI: 16px grid, 1.5px round strokes, drawn here so every glyph shares
// the same weight. Built with createElementNS (no innerHTML), colored by `currentColor`.

const NS = "http://www.w3.org/2000/svg";

const STROKES = {
  chevronRight: "m6 4 4 4-4 4",
  chevronDown: "m4 6 4 4 4-4",
  check: "m3.5 8.5 3 3 6-6.5",
  arrowUp: "M8 13V3M4 7l4-4 4 4",
  page: "M4 2.5h5l3 3v8H4v-11ZM9 2.5v3h3",
  copy: "M5.5 5.5h7v8h-7zM3.5 10.5V2.5h7",
  plus: "M8 3v10M3 8h10",
  close: "m4 4 8 8M12 4l-8 8",
  list: "M6 4h7M6 8h7M6 12h7M3 4h.01M3 8h.01M3 12h.01",
  pen: "M3 13l.7-3L10.5 3.2a1.4 1.4 0 0 1 2 2L5.7 12 3 13Z",
  chat: "M3 3.5h10v7H8.5L5.5 13v-2.5H3v-7Z",
  compose: "M7 3H3.5v9.5H13V9M12.5 2.5l1 1-5 5-1.5.5.5-1.5 5-5Z",
  bolt: "M9 1.5 3.5 9H8l-1 5.5L12.5 7H8l1-5.5Z",
} as const;

export type IconName = keyof typeof STROKES | "stop";

/** A new <svg> for the named icon. Decorative: hidden from assistive technology. */
export function icon(name: IconName, size = 16): SVGSVGElement {
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", "0 0 16 16");
  svg.setAttribute("width", String(size));
  svg.setAttribute("height", String(size));
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  if (name === "stop") {
    const r = document.createElementNS(NS, "rect");
    r.setAttribute("x", "4");
    r.setAttribute("y", "4");
    r.setAttribute("width", "8");
    r.setAttribute("height", "8");
    r.setAttribute("rx", "1.5");
    r.setAttribute("fill", "currentColor");
    svg.append(r);
    return svg;
  }
  const p = document.createElementNS(NS, "path");
  p.setAttribute("d", STROKES[name]);
  p.setAttribute("fill", "none");
  p.setAttribute("stroke", "currentColor");
  p.setAttribute("stroke-width", "1.5");
  p.setAttribute("stroke-linecap", "round");
  p.setAttribute("stroke-linejoin", "round");
  svg.append(p);
  return svg;
}
