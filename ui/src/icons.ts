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
  folder: "M2 4.5c0-.6.4-1 1-1h3l1.5 1.5H13c.6 0 1 .4 1 1V12c0 .6-.4 1-1 1H3c-.6 0-1-.4-1-1V4.5Z",
  folderPlus: "M2 4.5c0-.6.4-1 1-1h3l1.5 1.5H13c.6 0 1 .4 1 1V12c0 .6-.4 1-1 1H3c-.6 0-1-.4-1-1V4.5ZM8 7v4M6 9h4",
  file: "M4 2.5h5l3 3v8H4v-11ZM9 2.5v3h3",
  upload: "M8 11V3M4.5 6.5 8 3l3.5 3.5M3 13h10",
  trash: "M3.5 4.5h9M6.5 4.5V3h3v1.5M5 4.5l.5 8.5h5l.5-8.5",
  external: "M9 3h4v4M13 3 7.5 8.5M12 9.5V13H3V4h3.5",
  open: "M3 8h8M8 4.5 11.5 8 8 11.5",
  tag: "M2.5 8.5V3h5.5l5.5 5.5-5.5 5.5L2.5 8.5ZM5.5 5.5h.01",
  undo: "M5.5 3 3 5.5 5.5 8M3 5.5h6a3.5 3.5 0 0 1 0 7H6",
  chevronLeft: "m10 4-4 4 4 4",
  chevronUp: "m4 10 4-4 4 4",
  clock: "M8 2.5a5.5 5.5 0 1 0 0 11 5.5 5.5 0 0 0 0-11ZM8 5v3.2l2 1.3",
  grid: "M3 3h4v4H3zM9 3h4v4H9zM3 9h4v4H3zM9 9h4v4H9z",
  search: "M7 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8ZM10 10l3 3",
  refresh: "M12.5 8a4.5 4.5 0 1 1-1.4-3.3M12.5 2.5v2.7H9.8",
  download: "M8 3v8M4.5 7.5 8 11l3.5-3.5M3 13h10",
  image: "M2.5 3.5h11v9h-11zM2.5 10.5l3-3 3 3 2-2 3 3M10.5 6h.01",
  scissors: "M4.5 4a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3ZM4.5 9a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3ZM5.8 5.2 13 12M5.8 10.8 13 4",
  paste: "M5.5 3.5h-2v10h9v-10h-2M6 2.5h4v2H6zM6 8h4M6 10.5h4",
  info: "M8 2.5a5.5 5.5 0 1 0 0 11 5.5 5.5 0 0 0 0-11ZM8 7.5V11M8 5.3h.01",
  database: "M3 4c0-1 2.2-1.8 5-1.8s5 .8 5 1.8-2.2 1.8-5 1.8S3 5 3 4ZM3 4v8c0 1 2.2 1.8 5 1.8s5-.8 5-1.8V4M3 8c0 1 2.2 1.8 5 1.8S13 9 13 8",
  doc: "M4 2.5h5l3 3v8H4v-11ZM9 2.5v3h3M6 8.5h4M6 11h4",
  star: "M8 2.5l1.7 3.6 3.8.4-2.9 2.6.8 3.8L8 11l-3.4 1.9.8-3.8L2.5 6.5l3.8-.4L8 2.5Z",
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
