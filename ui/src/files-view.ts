// <ph-files>: the Files view, a file explorer over Phoenix's own library. The files live inside
// Phoenix's database, not in a folder on the computer, so there is nothing in Explorer to find:
// this view is where the user browses, uploads and organises them, and where the agent's saved
// files show up. Go owns the library and every rule about it; this element draws what Go sends
// and turns clicks, keys and drags into `f` messages. Names, notes and addresses come from files
// and from the agent, so they only ever reach the page through textContent.

import { el } from "./dom";
import { icon, type IconName } from "./icons";
import { send } from "./protocol";

interface Node {
  id: number;
  path: string;
  name: string;
  dir: boolean;
  size: number;
  mime?: string;
  kind?: string;
  note?: string;
  source?: string;
  origin?: string;
  created: string;
  modified: string;
  items?: number;
}

interface TrashRow {
  id: number;
  name: string;
  was: string;
  dir: boolean;
  size: number;
  deleted: string;
}

interface FilesData {
  loc: "dir" | "recent" | "kind" | "trash" | "search";
  dir: string;
  kind?: string;
  query?: string;
  entries: Node[];
  trashList?: TrashRow[];
  folders: string[];
  kinds: { kind: string; count: number }[];
  stats: { files: number; folders: number; bytes: number; trash: number };
  truncated?: boolean;
  exporting?: boolean;
  message?: string;
  error?: boolean;
  undo?: string;
  select?: string[];
  seq: number;
}

interface Preview {
  node: Node;
  image?: string;
  text?: string;
  more?: boolean;
  noOpen?: boolean;
  msg?: string;
  seq: number;
}

type Loc = { loc: FilesData["loc"]; arg: string };
type SortKey = "name" | "kind" | "modified" | "size";

interface Item {
  label?: string;
  icon?: IconName;
  run?: () => void;
  danger?: boolean;
  off?: boolean;
  hint?: string;
  sep?: boolean;
}

const CHUNK = 512 * 1024; // bytes of a dropped file per message
const MAX_DROP = 100 * 1024 * 1024;
const DRAG = "application/x-phoenix-files";

function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function when(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.valueOf())) return "";
  const days = Math.round((new Date().setHours(0, 0, 0, 0) - new Date(d).setHours(0, 0, 0, 0)) / 86_400_000);
  if (days <= 0) return `Today ${new Intl.DateTimeFormat(undefined, { timeStyle: "short" }).format(d)}`;
  if (days === 1) return "Yesterday";
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(d);
}

function full(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.valueOf()) ? "" : new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(d);
}

function glyph(n: { dir: boolean; mime?: string }): IconName {
  if (n.dir) return "folder";
  const m = n.mime ?? "";
  if (m.startsWith("image/")) return "image";
  if (m.startsWith("text/") || m.includes("json") || m.includes("pdf") || m.includes("word")) return "doc";
  return "file";
}

function nice(kind: string): string {
  return kind.replace(/-/g, " ");
}

function toBase64(buf: ArrayBuffer): string {
  const u = new Uint8Array(buf);
  let s = "";
  for (let i = 0; i < u.length; i += 0x8000) s += String.fromCharCode(...u.subarray(i, i + 0x8000));
  return btoa(s);
}

const baseName = (p: string): string => p.slice(p.lastIndexOf("/") + 1);
const parentOf = (p: string): string => (p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : "");

export class PhFiles extends HTMLElement {
  #data: FilesData | null = null;
  #sel = new Set<string>(); // paths (ids, in the trash)
  #anchor = "";
  #sort: { key: SortKey; asc: boolean } | null = null;
  #view: "list" | "grid" = localStorage.getItem("phx.files.view") === "grid" ? "grid" : "list";
  #pane = (localStorage.getItem("phx.files.pane") ?? (window.innerWidth >= 1500 ? "on" : "off")) === "on";
  #back: Loc[] = [];
  #fwd: Loc[] = [];
  #here: Loc = { loc: "dir", arg: "" };
  #open = new Set<string>(); // expanded folders in the tree
  #clip: { op: "cut" | "copy"; paths: string[] } | null = null;
  #renaming = "";
  #renameNext = false;
  #preview: Preview | null = null;
  #toast: number | undefined;
  #find: number | undefined;
  #confirmEmpty = false;

  readonly #side = el("aside", "fx-side");
  readonly #tools = el("div", "fx-tools");
  readonly #ribbon = el("div", "fx-ribbon");
  readonly #list = el("div", "fx-list");
  readonly #detail = el("aside", "fx-detail");
  readonly #status = el("div", "fx-status");
  readonly #toastEl = el("div", "fx-toast");
  readonly #dropEl = el("div", "fx-drop");
  readonly #menuEl = el("div", "fx-menu");
  readonly #search = el("input", "field fx-search");
  readonly #crumbs = el("nav", "fx-crumbs");
  readonly #backBtn = el("button", "fx-nav");
  readonly #fwdBtn = el("button", "fx-nav");
  readonly #upBtn = el("button", "fx-nav");
  readonly #viewBtn = el("button", "fx-nav");
  readonly #paneBtn = el("button", "fx-nav");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.dataset.pane = this.#pane ? "on" : "off";
    this.dataset.view = this.#view;

    this.#search.type = "search";
    this.#search.placeholder = "Search names, kinds, notes and text";
    this.#search.setAttribute("aria-label", "Search files");
    this.#search.autocomplete = "off";
    this.#search.spellcheck = false;
    this.#search.addEventListener("input", () => {
      window.clearTimeout(this.#find);
      this.#find = window.setTimeout(() => this.#runSearch(), 220);
    }, { passive: true });
    this.#search.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { window.clearTimeout(this.#find); this.#runSearch(); }
      if (e.key === "Escape") { this.#search.value = ""; this.#runSearch(); this.#list.focus(); }
    });

    const wire = (b: HTMLButtonElement, name: IconName, label: string, fn: () => void): void => {
      b.type = "button";
      b.title = label;
      b.setAttribute("aria-label", label);
      b.append(icon(name, 16));
      b.addEventListener("click", fn);
    };
    wire(this.#backBtn, "chevronLeft", "Back (Alt+Left)", () => this.#goBack());
    wire(this.#fwdBtn, "chevronRight", "Forward (Alt+Right)", () => this.#goForward());
    wire(this.#upBtn, "arrowUp", "Up one folder (Backspace)", () => this.#goUp());
    wire(this.#viewBtn, "grid", "Switch between list and icons", () => this.#toggleView());
    wire(this.#paneBtn, "info", "Show or hide details", () => this.#togglePane());

    const bar = el("div", "fx-bar");
    const group = el("div", "fx-navs");
    group.append(this.#backBtn, this.#fwdBtn, this.#upBtn);
    const right = el("div", "fx-navs");
    right.append(this.#viewBtn, this.#paneBtn);
    const searchWrap = el("label", "fx-searchwrap");
    searchWrap.append(icon("search", 14), this.#search);
    bar.append(group, this.#crumbs, searchWrap, right);
    this.#tools.append(bar);

    this.#list.tabIndex = 0;
    this.#list.setAttribute("role", "listbox");
    this.#list.setAttribute("aria-multiselectable", "true");
    this.#list.setAttribute("aria-label", "Files");
    this.#list.addEventListener("keydown", (e) => this.#onKey(e));
    this.#list.addEventListener("click", (e) => { if (e.target === this.#list) this.#select([]); });
    this.#list.addEventListener("contextmenu", (e) => {
      if (e.target !== this.#list && !(e.target as HTMLElement).classList.contains("fx-spacer")) return;
      e.preventDefault();
      this.#select([]);
      this.#menu(e.clientX, e.clientY, this.#blankMenu());
    });

    const main = el("div", "fx-main");
    const body = el("div", "fx-body");
    body.append(this.#list, this.#detail);
    main.append(this.#tools, this.#ribbon, body, this.#status);

    this.#toastEl.setAttribute("role", "status");
    this.#toastEl.hidden = true;
    this.#menuEl.hidden = true;
    this.#menuEl.setAttribute("role", "menu");
    this.#dropEl.hidden = true;
    this.#dropEl.append(icon("upload", 28), el("strong", undefined, "Drop to add to this folder"));

    this.append(this.#side, main, this.#toastEl, this.#menuEl, this.#dropEl);
    this.#bindDrop();
    document.addEventListener("pointerdown", (e) => { if (!this.#menuEl.contains(e.target as globalThis.Node)) this.#closeMenu(); }, true);
    window.addEventListener("blur", () => this.#closeMenu());
    this.addEventListener("keydown", (e) => {
      if (e.key === "Escape") this.#closeMenu();
      if (e.altKey && e.key === "ArrowLeft") { e.preventDefault(); this.#goBack(); }
      if (e.altKey && e.key === "ArrowRight") { e.preventDefault(); this.#goForward(); }
    });
    this.#renderAll();
  }

  // ── data from Go ────────────────────────────────────────────────────────

  set data(value: FilesData) {
    const prev = this.#data;
    this.#data = value;
    this.#here = { loc: value.loc, arg: value.loc === "dir" ? value.dir : value.loc === "kind" ? value.kind ?? "" : value.loc === "search" ? value.query ?? "" : "" };
    if (value.loc === "dir") for (let d = value.dir; d; d = parentOf(d)) this.#open.add(parentOf(d) === d ? "" : parentOf(d));
    if (value.loc !== "search" && document.activeElement !== this.#search) this.#search.value = "";
    if (value.loc === "search" && document.activeElement !== this.#search) this.#search.value = value.query ?? "";

    const keys = new Set(value.loc === "trash" ? (value.trashList ?? []).map((t) => String(t.id)) : value.entries.map((n) => n.path));
    let next: string[] = [...this.#sel].filter((k) => keys.has(k));
    if (value.select?.length) next = value.select.filter((k) => keys.has(k));
    this.#sel = new Set(next);
    if (!next.includes(this.#anchor)) this.#anchor = next[0] ?? "";
    if (prev && prev.loc !== value.loc) this.#renaming = "";
    this.#preview = null;
    this.#confirmEmpty = false;

    this.#renderAll();
    this.#syncPreview();
    if (this.#renameNext && next.length === 1) { this.#renameNext = false; this.#startRename(next[0] ?? ""); }
    this.#showToast(value);
  }

  set preview(value: Preview) {
    const only = this.#only();
    if (!only || value.node.path !== only) return;
    this.#preview = value;
    this.#renderDetail();
  }

  // ── navigation ──────────────────────────────────────────────────────────

  #go(loc: Loc["loc"], arg = "", record = true): void {
    if (record && !(this.#here.loc === loc && this.#here.arg === arg)) {
      this.#back.push(this.#here);
      this.#fwd = [];
    }
    this.#sel.clear();
    this.#anchor = "";
    this.#renaming = "";
    if (loc !== "search") this.#search.value = "";
    send("f", "nav", loc, arg);
  }

  #goBack(): void {
    const to = this.#back.pop();
    if (!to) return;
    this.#fwd.push(this.#here);
    this.#go(to.loc, to.arg, false);
  }

  #goForward(): void {
    const to = this.#fwd.pop();
    if (!to) return;
    this.#back.push(this.#here);
    this.#go(to.loc, to.arg, false);
  }

  #goUp(): void {
    if (this.#here.loc !== "dir") { this.#go("dir", ""); return; }
    if (this.#here.arg) this.#go("dir", parentOf(this.#here.arg));
  }

  #runSearch(): void {
    const q = this.#search.value.trim();
    if (q) this.#go("search", q, this.#here.loc !== "search");
    else if (this.#here.loc === "search") this.#go("dir", this.#lastDir(), false);
  }

  #lastDir(): string {
    for (let i = this.#back.length - 1; i >= 0; i--) { const b = this.#back[i]; if (b && b.loc === "dir") return b.arg; }
    return "";
  }

  #toggleView(): void {
    this.#view = this.#view === "list" ? "grid" : "list";
    localStorage.setItem("phx.files.view", this.#view);
    this.dataset.view = this.#view;
    this.#renderList();
  }

  #togglePane(): void {
    this.#pane = !this.#pane;
    localStorage.setItem("phx.files.pane", this.#pane ? "on" : "off");
    this.dataset.pane = this.#pane ? "on" : "off";
    this.#syncPreview();
  }

  // ── what is on screen ───────────────────────────────────────────────────

  #rows(): Node[] {
    const d = this.#data;
    if (!d) return [];
    const rows = [...d.entries];
    const sort = this.#sort ?? (d.loc === "recent" ? { key: "modified" as SortKey, asc: false } : d.loc === "dir" ? { key: "name" as SortKey, asc: true } : null);
    if (!sort) return rows;
    const dir = sort.asc ? 1 : -1;
    const cmp: Record<SortKey, (a: Node, b: Node) => number> = {
      name: (a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" }),
      kind: (a, b) => (a.kind ?? "").localeCompare(b.kind ?? "") || a.name.localeCompare(b.name),
      modified: (a, b) => a.modified.localeCompare(b.modified),
      size: (a, b) => a.size - b.size,
    };
    return rows.sort((a, b) => (a.dir === b.dir ? dir * cmp[sort.key](a, b) : a.dir ? -1 : 1));
  }

  #byPath(p: string): Node | undefined {
    return this.#data?.entries.find((n) => n.path === p);
  }

  #only(): string {
    return this.#sel.size === 1 ? ([...this.#sel][0] ?? "") : "";
  }

  #renderAll(): void {
    this.#renderSide();
    this.#renderTools();
    this.#renderRibbon();
    this.#renderList();
    this.#renderStatus();
    this.#renderDetail();
  }

  // ── sidebar ─────────────────────────────────────────────────────────────

  #renderSide(): void {
    const d = this.#data;
    const here = this.#here;
    this.#side.replaceChildren();

    const head = el("div", "fx-side-head");
    const mark = el("span", "fx-side-mark");
    mark.append(icon("database", 16));
    const t = el("div", "fx-side-title");
    t.append(el("strong", undefined, "Files"), el("span", undefined, "Kept inside Phoenix"));
    head.append(mark, t);

    const nav = el("nav", "fx-quick");
    nav.setAttribute("aria-label", "Locations");
    const quick = (name: IconName, label: string, loc: Loc["loc"], arg: string, count?: number, on = false): void => {
      const b = el("button", "fx-item");
      b.type = "button";
      b.append(icon(name, 16), el("span", "fx-item-name", label));
      if (count !== undefined && count > 0) b.append(el("span", "fx-count", String(count)));
      if (on) b.setAttribute("aria-current", "page");
      b.addEventListener("click", () => this.#go(loc, arg));
      this.#dropTarget(b, loc === "dir" ? arg : null);
      nav.append(b);
    };
    quick("database", "All files", "dir", "", undefined, here.loc === "dir" && here.arg === "");
    quick("clock", "Recent", "recent", "", undefined, here.loc === "recent");

    const kinds = el("div", "fx-group");
    if (d?.kinds.length) {
      kinds.append(el("h3", "fx-group-title", "What they are"));
      for (const k of d.kinds) {
        const b = el("button", "fx-item");
        b.type = "button";
        b.append(icon("tag", 16), el("span", "fx-item-name", nice(k.kind)), el("span", "fx-count", String(k.count)));
        if (here.loc === "kind" && here.arg === k.kind) b.setAttribute("aria-current", "page");
        b.addEventListener("click", () => this.#go("kind", k.kind));
        kinds.append(b);
      }
    }

    const tree = el("div", "fx-group");
    tree.append(el("h3", "fx-group-title", "Folders"));
    const tr = el("div", "fx-tree");
    tr.setAttribute("role", "tree");
    const folders = d?.folders ?? [];
    if (!folders.length) tr.append(el("p", "fx-tree-empty", "No folders yet"));
    else this.#drawTree(tr, folders, "", 0);
    tree.append(tr);

    const foot = el("div", "fx-side-foot");
    const trash = el("button", "fx-item");
    trash.type = "button";
    trash.append(icon("trash", 16), el("span", "fx-item-name", "Trash"));
    if (d && d.stats.trash > 0) trash.append(el("span", "fx-count", String(d.stats.trash)));
    if (here.loc === "trash") trash.setAttribute("aria-current", "page");
    trash.addEventListener("click", () => this.#go("trash"));
    const usage = el("p", "fx-usage");
    if (d) usage.textContent = `${d.stats.files} ${d.stats.files === 1 ? "file" : "files"} · ${bytes(d.stats.bytes)}`;
    foot.append(trash, usage);

    const scroll = el("div", "fx-side-scroll");
    scroll.append(nav, kinds, tree);
    this.#side.append(head, scroll, foot);
  }

  #drawTree(into: HTMLElement, folders: string[], parent: string, depth: number): void {
    const kids = folders.filter((f) => parentOf(f) === parent).sort((a, b) => baseName(a).localeCompare(baseName(b), undefined, { numeric: true, sensitivity: "base" }));
    for (const f of kids) {
      const hasKids = folders.some((x) => parentOf(x) === f);
      const open = this.#open.has(f);
      const row = el("div", "fx-node");
      row.style.setProperty("--depth", String(depth));
      row.setAttribute("role", "treeitem");
      if (hasKids) row.setAttribute("aria-expanded", String(open));
      const twist = el("button", "fx-twist");
      twist.type = "button";
      twist.tabIndex = -1;
      twist.setAttribute("aria-label", open ? "Collapse" : "Expand");
      if (hasKids) twist.append(icon(open ? "chevronDown" : "chevronRight", 12));
      twist.addEventListener("click", (e) => {
        e.stopPropagation();
        if (open) this.#open.delete(f); else this.#open.add(f);
        this.#renderSide();
      });
      const b = el("button", "fx-item fx-leaf");
      b.type = "button";
      b.append(icon("folder", 16), el("span", "fx-item-name", baseName(f)));
      if (this.#here.loc === "dir" && this.#here.arg === f) b.setAttribute("aria-current", "page");
      b.addEventListener("click", () => this.#go("dir", f));
      this.#dropTarget(b, f);
      row.append(twist, b);
      into.append(row);
      if (hasKids && open) this.#drawTree(into, folders, f, depth + 1);
    }
  }

  // ── toolbar and path bar ────────────────────────────────────────────────

  #renderTools(): void {
    const d = this.#data;
    this.#backBtn.disabled = this.#back.length === 0;
    this.#fwdBtn.disabled = this.#fwd.length === 0;
    this.#upBtn.disabled = this.#here.loc === "dir" && this.#here.arg === "";
    this.#viewBtn.replaceChildren(icon(this.#view === "list" ? "grid" : "list", 16));
    this.#viewBtn.setAttribute("aria-pressed", String(this.#view === "grid"));
    this.#paneBtn.setAttribute("aria-pressed", String(this.#pane));

    this.#crumbs.replaceChildren();
    this.#crumbs.setAttribute("aria-label", "Location");
    const crumb = (label: string, path: string | null, last: boolean, glyphName?: IconName): void => {
      const b = el("button", last ? "fx-crumb fx-crumb-now" : "fx-crumb");
      b.type = "button";
      if (glyphName) b.append(icon(glyphName, 14));
      b.append(el("span", undefined, label));
      if (last) b.setAttribute("aria-current", "location");
      if (path !== null) {
        b.addEventListener("click", () => this.#go("dir", path));
        this.#dropTarget(b, path);
      } else b.disabled = true;
      if (this.#crumbs.childElementCount) this.#crumbs.append(el("span", "fx-sep", "›"));
      this.#crumbs.append(b);
    };
    const loc = d?.loc ?? "dir";
    if (loc === "dir") {
      const segs = (d?.dir ?? "").split("/").filter(Boolean);
      crumb("Files", "", segs.length === 0, "database");
      let acc = "";
      segs.forEach((s, i) => { acc = acc ? `${acc}/${s}` : s; crumb(s, acc, i === segs.length - 1); });
    } else {
      crumb("Files", "", false, "database");
      const label = loc === "recent" ? "Recent" : loc === "trash" ? "Trash" : loc === "kind" ? nice(d?.kind ?? "") : `Search “${d?.query ?? ""}”`;
      crumb(label, null, true);
    }
  }

  // ── ribbon ──────────────────────────────────────────────────────────────

  #renderRibbon(): void {
    this.#ribbon.replaceChildren();
    const d = this.#data;
    const trash = d?.loc === "trash";
    const n = this.#sel.size;
    const one = this.#only();
    const node = one ? this.#byPath(one) : undefined;
    const add = (label: string, name: IconName, run: () => void, o: { primary?: boolean; off?: boolean; danger?: boolean; hint?: string } = {}): void => {
      const b = el("button", o.primary ? "fx-act fx-act-primary" : o.danger ? "fx-act fx-act-danger" : "fx-act");
      b.type = "button";
      b.disabled = !!o.off;
      if (o.hint) b.title = `${label} (${o.hint})`;
      b.append(icon(name, 16), el("span", undefined, label));
      b.addEventListener("click", run);
      this.#ribbon.append(b);
    };
    const sep = (): void => { this.#ribbon.append(el("span", "fx-ribbon-sep")); };

    if (trash) {
      add("Restore", "undo", () => send("f", "restore", ...this.#ids()), { primary: true, off: n === 0 });
      add("Delete forever", "trash", () => this.#purge(), { danger: true, off: n === 0 });
      sep();
      add(this.#confirmEmpty ? "Click again to empty" : "Empty trash", "trash", () => this.#emptyTrash(), { off: !d?.trashList?.length, danger: this.#confirmEmpty });
      return;
    }
    const inDir = !d || d.loc === "dir";
    add("Add files", "upload", () => send("f", "upload", this.#target()), { primary: true });
    add("New folder", "folderPlus", () => this.#newFolder(), { off: !inDir });
    sep();
    add("Open", "open", () => this.#openNode(node), { off: !node || (!node.dir && !!this.#preview?.noOpen) });
    add("Rename", "pen", () => this.#startRename(one), { off: n !== 1, hint: "F2" });
    add("Copy", "copy", () => this.#copy("copy"), { off: n === 0, hint: "Ctrl+C" });
    add("Cut", "scissors", () => this.#copy("cut"), { off: n === 0, hint: "Ctrl+X" });
    add("Paste", "paste", () => this.#paste(this.#target()), { off: !this.#clip || !inDir, hint: "Ctrl+V" });
    sep();
    add(d?.exporting ? "Exporting…" : "Export…", "download", () => send("f", "export", ...this.#ids()), { off: n === 0 || d?.exporting === true });
    add("Delete", "trash", () => this.#trashSelected(), { off: n === 0, danger: true, hint: "Delete" });
  }

  // ── the list ────────────────────────────────────────────────────────────

  #renderList(): void {
    const d = this.#data;
    this.#list.replaceChildren();
    if (!d) return;
    if (d.loc === "trash") { this.#renderTrash(d); return; }
    const rows = this.#rows();
    if (!rows.length) { this.#list.append(this.#empty(d)); return; }

    if (this.#view === "list") {
      const head = el("div", "fx-row fx-head");
      head.setAttribute("role", "row");
      const col = (label: string, key: SortKey | null, cls: string): void => {
        const c = el(key ? "button" : "span", `fx-col ${cls}`);
        if (key) {
          const b = c as HTMLButtonElement;
          b.type = "button";
          b.append(el("span", undefined, label));
          const active = this.#sort?.key === key || (!this.#sort && ((d.loc === "dir" && key === "name") || (d.loc === "recent" && key === "modified")));
          if (active) {
            const asc = this.#sort ? this.#sort.asc : d.loc !== "recent";
            b.append(icon(asc ? "chevronUp" : "chevronDown", 12));
            b.setAttribute("aria-sort", asc ? "ascending" : "descending");
          }
          b.addEventListener("click", () => {
            const cur = this.#sort;
            this.#sort = cur?.key === key ? { key, asc: !cur.asc } : { key, asc: key === "name" || key === "kind" };
            this.#renderList();
          });
        } else c.textContent = label;
        head.append(c);
      };
      col("Name", "name", "fx-c-name");
      col("Kind", "kind", "fx-c-kind");
      col("Modified", "modified", "fx-c-when");
      col("Size", "size", "fx-c-size");
      this.#list.append(head);
    }

    const cut = new Set(this.#clip?.op === "cut" ? this.#clip.paths : []);
    for (const n of rows) this.#list.append(this.#row(n, cut.has(n.path)));
    if (d.truncated) this.#list.append(el("p", "fx-more", "Only the first part of this folder is shown. Use search to find the rest."));
    this.#list.append(el("div", "fx-spacer"));
  }

  #row(n: Node, cut: boolean): HTMLElement {
    const r = el("div", "fx-row fx-file");
    r.dataset.path = n.path;
    r.setAttribute("role", "option");
    r.setAttribute("aria-selected", String(this.#sel.has(n.path)));
    if (cut) r.dataset.cut = "true";
    r.draggable = this.#renaming !== n.path;

    const name = el("div", "fx-c-name fx-name");
    const mark = el("span", n.dir ? "fx-mark fx-mark-dir" : "fx-mark");
    mark.append(icon(glyph(n), this.#view === "grid" ? 28 : 18));
    name.append(mark);
    if (this.#renaming === n.path) {
      name.append(this.#renameField(n));
    } else {
      const label = el("span", "fx-label", n.name);
      label.title = n.name;
      name.append(label);
    }
    r.append(name);

    if (this.#view === "list") {
      const k = el("div", "fx-c-kind");
      if (n.kind) k.append(el("span", "fx-tag", nice(n.kind)));
      else k.append(el("span", "fx-dim", n.dir ? "Folder" : "File"));
      r.append(k, el("div", "fx-c-when fx-dim", when(n.modified)), el("div", "fx-c-size fx-dim", n.dir ? `${n.items ?? 0} ${n.items === 1 ? "item" : "items"}` : bytes(n.size)));
    } else if (n.kind) {
      r.append(el("span", "fx-tag", nice(n.kind)));
    }
    if (n.path.includes("/") && this.#data?.loc !== "dir") {
      const where = el("div", "fx-where fx-dim", `in ${parentOf(n.path)}`);
      name.append(where);
    }

    r.addEventListener("click", (e) => this.#click(e, n.path));
    r.addEventListener("dblclick", () => { if (this.#renaming !== n.path) this.#openNode(n); });
    r.addEventListener("contextmenu", (e) => {
      e.preventDefault();
      if (!this.#sel.has(n.path)) this.#select([n.path]);
      this.#menu(e.clientX, e.clientY, this.#rowMenu(n));
    });
    r.addEventListener("dragstart", (e) => {
      if (!this.#sel.has(n.path)) this.#select([n.path]);
      const paths = [...this.#sel];
      e.dataTransfer?.setData(DRAG, JSON.stringify(paths));
      e.dataTransfer?.setData("text/plain", paths.map(baseName).join("\n"));
      if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
      this.#list.querySelectorAll<HTMLElement>("[aria-selected=true]").forEach((x) => { x.dataset.dragging = "true"; });
    });
    r.addEventListener("dragend", () => {
      this.#list.querySelectorAll<HTMLElement>("[data-dragging]").forEach((x) => { delete x.dataset.dragging; });
      this.querySelectorAll<HTMLElement>("[data-over]").forEach((x) => { delete x.dataset.over; });
    });
    if (n.dir) this.#dropTarget(r, n.path);
    return r;
  }

  #renderTrash(d: FilesData): void {
    const items = d.trashList ?? [];
    if (!items.length) { this.#list.append(this.#empty(d)); return; }
    const head = el("div", "fx-row fx-head fx-trash");
    for (const [label, cls] of [["Name", "fx-c-name"], ["Was in", "fx-c-kind"], ["Deleted", "fx-c-when"], ["Size", "fx-c-size"]] as const) head.append(el("span", `fx-col ${cls}`, label));
    this.#list.append(head);
    for (const t of items) {
      const key = String(t.id);
      const r = el("div", "fx-row fx-file fx-trash");
      r.dataset.path = key;
      r.setAttribute("role", "option");
      r.setAttribute("aria-selected", String(this.#sel.has(key)));
      const name = el("div", "fx-c-name fx-name");
      const mark = el("span", t.dir ? "fx-mark fx-mark-dir" : "fx-mark");
      mark.append(icon(t.dir ? "folder" : "file", 18));
      name.append(mark, el("span", "fx-label", t.name));
      const was = t.was.includes("/") ? parentOf(t.was) : "Top level";
      r.append(name, el("div", "fx-c-kind fx-dim", was), el("div", "fx-c-when fx-dim", when(t.deleted)), el("div", "fx-c-size fx-dim", t.dir ? "Folder" : bytes(t.size)));
      r.addEventListener("click", (e) => this.#click(e, key));
      r.addEventListener("dblclick", () => send("f", "restore", key));
      r.addEventListener("contextmenu", (e) => {
        e.preventDefault();
        if (!this.#sel.has(key)) this.#select([key]);
        this.#menu(e.clientX, e.clientY, [
          { label: "Restore", icon: "undo", run: () => send("f", "restore", ...this.#ids()) },
          { sep: true },
          { label: "Delete forever", icon: "trash", danger: true, run: () => this.#purge() },
        ]);
      });
      this.#list.append(r);
    }
  }

  #empty(d: FilesData): HTMLElement {
    const box = el("div", "fx-empty");
    const mark = el("span", "fx-empty-mark");
    let title = "This folder is empty";
    let text = "Drop files here, or add them. Anything the agent saves to this place will show up as well.";
    let name: IconName = "folder";
    let action: [string, () => void] | null = ["Add files", () => send("f", "upload", d.dir)];
    if (d.loc === "trash") { title = "The trash is empty"; text = "Deleted files wait here until you restore them or empty the trash. Nothing is lost by deleting."; name = "trash"; action = null; }
    else if (d.loc === "recent") { title = "Nothing yet"; text = "Files you add and files the agent saves will appear here, newest first."; name = "clock"; action = null; }
    else if (d.loc === "search") { title = "No matches"; text = `Nothing matches “${d.query ?? ""}”. Searches look at names, kinds, notes and the text inside files.`; name = "search"; action = null; }
    else if (d.loc === "kind") { title = `No ${nice(d.kind ?? "")} files`; text = "When the agent saves a file of this kind, or you tag one, it will be listed here."; name = "tag"; action = null; }
    else if (d.loc === "dir" && d.dir === "" && d.stats.files === 0 && d.stats.folders === 0) {
      title = "Your library is empty";
      text = "This is where Phoenix keeps files, inside the app. When the agent gets a resume, receipt or download for you, it saves it here and remembers what it is, so it can use it again. Add your own too.";
    }
    mark.append(icon(name, 26));
    box.append(mark, el("h3", undefined, title), el("p", undefined, text));
    if (action) {
      const b = el("button", "btn btn-primary");
      b.type = "button";
      b.textContent = action[0];
      b.addEventListener("click", action[1]);
      box.append(b);
    }
    return box;
  }

  // ── status ──────────────────────────────────────────────────────────────

  #renderStatus(): void {
    const d = this.#data;
    this.#status.replaceChildren();
    if (!d) return;
    const total = d.loc === "trash" ? (d.trashList?.length ?? 0) : d.entries.length;
    const left = el("span", undefined, `${total} ${total === 1 ? "item" : "items"}`);
    this.#status.append(left);
    if (this.#sel.size) {
      let size = 0;
      let files = 0;
      for (const k of this.#sel) { const n = this.#byPath(k); if (n && !n.dir) { size += n.size; files++; } }
      this.#status.append(el("span", undefined, `${this.#sel.size} selected${files ? ` · ${bytes(size)}` : ""}`));
    }
    this.#status.append(el("span", "fx-status-end", "Stored inside Phoenix, not on your disk"));
  }

  // ── details pane ────────────────────────────────────────────────────────

  #syncPreview(): void {
    const one = this.#only();
    const n = one ? this.#byPath(one) : undefined;
    this.#preview = null;
    if (n && !n.dir && this.#pane) send("f", "preview", n.path);
    this.#renderDetail();
  }

  #renderDetail(): void {
    const d = this.#data;
    this.#detail.replaceChildren();
    this.#detail.hidden = !this.#pane;
    if (!d || !this.#pane) return;

    if (d.loc === "trash") {
      this.#detail.append(this.#note("Trash", "Select an item to restore it, or delete it for good. Folders come back with everything in them."));
      return;
    }
    if (this.#sel.size > 1) {
      let size = 0;
      for (const k of this.#sel) { const n = this.#byPath(k); if (n && !n.dir) size += n.size; }
      this.#detail.append(this.#note(`${this.#sel.size} items selected`, `${bytes(size)} in files. Drag them onto a folder to move them, or use the buttons above.`));
      return;
    }
    const one = this.#only();
    const n = one ? this.#byPath(one) : undefined;
    if (!n) {
      const where = d.loc === "dir" ? (d.dir ? baseName(d.dir) : "All files") : "Files";
      this.#detail.append(this.#note(where, `${d.stats.files} ${d.stats.files === 1 ? "file" : "files"} and ${d.stats.folders} ${d.stats.folders === 1 ? "folder" : "folders"} · ${bytes(d.stats.bytes)}. Select something to see what it is and what the agent knows about it.`));
      return;
    }

    const card = el("div", "fx-card");
    const top = el("div", "fx-prev");
    const pv = this.#preview && this.#preview.node.path === n.path ? this.#preview : null;
    if (pv?.image) {
      const img = el("img", "fx-img");
      img.alt = n.name;
      img.src = pv.image;
      top.append(img);
    } else if (pv?.text) {
      const pre = el("pre", "fx-text");
      pre.textContent = pv.text + (pv.more ? "\n…" : "");
      top.append(pre);
    } else {
      const big = el("span", "fx-bigmark");
      big.append(icon(glyph(n), 34));
      top.append(big);
    }
    card.append(top);

    const title = el("h3", "fx-dname", n.name);
    title.title = n.name;
    card.append(title);

    const meta = el("dl", "fx-meta");
    const row = (k: string, v: string): void => { if (v) meta.append(el("dt", undefined, k), el("dd", undefined, v)); };
    row("Size", n.dir ? `${n.items ?? 0} ${n.items === 1 ? "item" : "items"}` : bytes(n.size));
    row("Type", n.dir ? "Folder" : n.mime || "File");
    row("Modified", full(n.modified));
    row("Added", full(n.created));
    row("By", n.origin === "agent" ? "The agent" : "You");
    row("Where", n.path.includes("/") ? parentOf(n.path) : "Top level");
    if (n.source) row("From", n.source);
    card.append(meta);

    if (!n.dir) {
      const form = el("form", "fx-form");
      const kindId = "fx-kinds";
      const lk = el("label", "fx-field");
      lk.append(el("span", undefined, "What it is"));
      const kind = el("input", "field");
      kind.value = n.kind ?? "";
      kind.placeholder = "resume, receipt, notes…";
      kind.maxLength = 40;
      kind.setAttribute("list", kindId);
      kind.autocomplete = "off";
      const dl = el("datalist");
      dl.id = kindId;
      for (const k of new Set(["resume", "cover-letter", "receipt", "notes", "research", ...d.kinds.map((x) => x.kind)])) { const o = el("option"); o.value = k; dl.append(o); }
      lk.append(kind, dl);
      const ln = el("label", "fx-field");
      ln.append(el("span", undefined, "Note for the agent and for you"));
      const note = el("textarea", "field");
      note.value = n.note ?? "";
      note.rows = 4;
      note.maxLength = 400;
      note.placeholder = "A line that helps find it again";
      ln.append(note);
      const save = el("button", "btn btn-sm");
      save.type = "submit";
      save.textContent = "Save";
      save.disabled = true;
      const dirty = (): void => { save.disabled = kind.value.trim() === (n.kind ?? "") && note.value.trim() === (n.note ?? ""); };
      kind.addEventListener("input", dirty);
      note.addEventListener("input", dirty);
      form.addEventListener("submit", (e) => { e.preventDefault(); send("f", "info", n.path, kind.value.trim(), note.value.trim()); });
      form.append(lk, ln, save);
      card.append(form);
    }
    this.#detail.append(card);
  }

  #note(title: string, text: string): HTMLElement {
    const box = el("div", "fx-card fx-card-note");
    box.append(el("h3", undefined, title), el("p", undefined, text));
    return box;
  }

  // ── selection ───────────────────────────────────────────────────────────

  #keys(): string[] {
    const d = this.#data;
    if (!d) return [];
    return d.loc === "trash" ? (d.trashList ?? []).map((t) => String(t.id)) : this.#rows().map((n) => n.path);
  }

  #ids(): string[] {
    return [...this.#sel];
  }

  #select(keys: string[], anchor?: string): void {
    this.#sel = new Set(keys);
    if (anchor !== undefined) this.#anchor = anchor;
    else if (!keys.includes(this.#anchor)) this.#anchor = keys[0] ?? "";
    if (this.#renaming && !this.#sel.has(this.#renaming)) this.#renaming = "";
    this.#paint();
    this.#renderRibbon();
    this.#renderStatus();
    this.#syncPreview();
  }

  #paint(): void {
    this.#list.querySelectorAll<HTMLElement>(".fx-file").forEach((r) => r.setAttribute("aria-selected", String(this.#sel.has(r.dataset.path ?? ""))));
  }

  #click(e: MouseEvent, key: string): void {
    this.#closeMenu();
    if ((e.target as HTMLElement).closest("input")) return;
    if (e.shiftKey && this.#anchor) {
      const all = this.#keys();
      const a = all.indexOf(this.#anchor);
      const b = all.indexOf(key);
      if (a >= 0 && b >= 0) { this.#select(all.slice(Math.min(a, b), Math.max(a, b) + 1), this.#anchor); return; }
    }
    if (e.ctrlKey || e.metaKey) {
      const next = new Set(this.#sel);
      if (next.has(key)) next.delete(key); else next.add(key);
      this.#select([...next], key);
      return;
    }
    this.#select([key], key);
    this.#list.focus({ preventScroll: true });
  }

  #onKey(e: KeyboardEvent): void {
    if ((e.target as HTMLElement).closest("input,textarea")) return;
    const keys = this.#keys();
    const mod = e.ctrlKey || e.metaKey;
    const move = (delta: number): void => {
      if (!keys.length) return;
      const cur = keys.indexOf(this.#anchor || [...this.#sel][0] || "");
      const next = keys[Math.max(0, Math.min(keys.length - 1, (cur < 0 ? (delta > 0 ? -1 : keys.length) : cur) + delta))] ?? "";
      if (e.shiftKey && this.#anchor) {
        const a = keys.indexOf(this.#anchor);
        const b = keys.indexOf(next);
        this.#select(keys.slice(Math.min(a, b), Math.max(a, b) + 1), this.#anchor);
      } else this.#select([next], next);
      this.#list.querySelector<HTMLElement>(`[data-path="${CSS.escape(next)}"]`)?.scrollIntoView({ block: "nearest" });
    };
    switch (true) {
      case e.key === "ArrowDown": e.preventDefault(); move(1); break;
      case e.key === "ArrowUp": e.preventDefault(); move(-1); break;
      case e.key === "Home": e.preventDefault(); if (keys[0]) this.#select([keys[0]], keys[0]); break;
      case e.key === "End": e.preventDefault(); { const k = keys[keys.length - 1]; if (k) this.#select([k], k); } break;
      case e.key === "Enter": { e.preventDefault(); const n = this.#byPath(this.#only()); if (n) this.#openNode(n); else if (this.#data?.loc === "trash" && this.#sel.size) send("f", "restore", ...this.#ids()); break; }
      case e.key === "F2": e.preventDefault(); this.#startRename(this.#only()); break;
      case e.key === "Delete": e.preventDefault(); if (this.#data?.loc === "trash") this.#purge(); else this.#trashSelected(); break;
      case e.key === "Backspace": e.preventDefault(); this.#goUp(); break;
      case e.key === "Escape": this.#select([]); break;
      case mod && e.key.toLowerCase() === "a": e.preventDefault(); this.#select(keys, keys[0]); break;
      case mod && e.key.toLowerCase() === "c": e.preventDefault(); this.#copy("copy"); break;
      case mod && e.key.toLowerCase() === "x": e.preventDefault(); this.#copy("cut"); break;
      case mod && e.key.toLowerCase() === "v": e.preventDefault(); this.#paste(this.#target()); break;
      case mod && e.key.toLowerCase() === "f": e.preventDefault(); this.#search.focus(); this.#search.select(); break;
      default: break;
    }
  }

  // ── actions ─────────────────────────────────────────────────────────────

  /** The folder an action lands in: the one shown, or the folder selected inside it. */
  #target(): string {
    return this.#data?.loc === "dir" ? this.#data.dir : "";
  }

  #openNode(n?: Node): void {
    if (!n) return;
    if (n.dir) { this.#go("dir", n.path); return; }
    send("f", "open", n.path);
  }

  #newFolder(): void {
    const d = this.#data;
    if (!d || d.loc !== "dir") return;
    const taken = new Set(d.entries.map((n) => n.name.toLowerCase()));
    let name = "New folder";
    for (let i = 2; taken.has(name.toLowerCase()); i++) name = `New folder ${i}`;
    this.#renameNext = true;
    send("f", "mkdir", d.dir ? `${d.dir}/${name}` : name);
  }

  #startRename(path: string): void {
    if (!path || this.#data?.loc === "trash") return;
    this.#renaming = path;
    this.#renderList();
    const input = this.#list.querySelector<HTMLInputElement>(".fx-rename");
    if (!input) return;
    input.focus();
    const dot = input.value.lastIndexOf(".");
    const n = this.#byPath(path);
    input.setSelectionRange(0, n && !n.dir && dot > 0 ? dot : input.value.length);
    input.scrollIntoView({ block: "nearest" });
  }

  #renameField(n: Node): HTMLInputElement {
    const input = el("input", "field fx-rename");
    input.value = n.name;
    input.maxLength = 120;
    input.spellcheck = false;
    input.setAttribute("aria-label", `New name for ${n.name}`);
    let done = false;
    const finish = (commit: boolean): void => {
      if (done) return;
      done = true;
      this.#renaming = "";
      const v = input.value.trim();
      if (commit && v && v !== n.name) send("f", "rename", n.path, v);
      else this.#renderList();
      this.#list.focus({ preventScroll: true });
    };
    input.addEventListener("keydown", (e) => {
      e.stopPropagation();
      if (e.key === "Enter") finish(true);
      if (e.key === "Escape") finish(false);
    });
    input.addEventListener("blur", () => finish(true));
    input.addEventListener("click", (e) => e.stopPropagation());
    input.addEventListener("dblclick", (e) => e.stopPropagation());
    input.addEventListener("dragstart", (e) => e.preventDefault());
    return input;
  }

  #copy(op: "cut" | "copy"): void {
    if (this.#data?.loc === "trash" || !this.#sel.size) return;
    this.#clip = { op, paths: [...this.#sel] };
    this.#renderList();
    this.#renderRibbon();
    this.#notify(op === "cut" ? `${this.#sel.size} ready to move. Open a folder and paste.` : `${this.#sel.size} copied. Open a folder and paste.`);
  }

  #paste(dir: string): void {
    const c = this.#clip;
    if (!c) return;
    send("f", c.op === "cut" ? "move" : "copy", dir, ...c.paths);
    if (c.op === "cut") this.#clip = null;
  }

  #trashSelected(): void {
    if (!this.#sel.size || this.#data?.loc === "trash") return;
    send("f", "delete", ...this.#ids());
  }

  #purge(): void {
    if (!this.#sel.size) return;
    for (const id of this.#sel) send("f", "purge", id);
  }

  #emptyTrash(): void {
    if (!this.#confirmEmpty) {
      this.#confirmEmpty = true;
      this.#renderRibbon();
      window.setTimeout(() => { this.#confirmEmpty = false; this.#renderRibbon(); }, 4000);
      return;
    }
    this.#confirmEmpty = false;
    send("f", "empty-trash");
  }

  // ── context menus ───────────────────────────────────────────────────────

  #rowMenu(n: Node): Item[] {
    const many = this.#sel.size > 1;
    const noOpen = !n.dir && !!this.#preview?.noOpen;
    const items: Item[] = [
      { label: n.dir ? "Open folder" : "Open", icon: "open", run: () => this.#openNode(n), off: many || noOpen },
      { label: "Export…", icon: "download", run: () => send("f", "export", ...this.#ids()), off: this.#data?.exporting === true },
      { sep: true },
      { label: "Rename", icon: "pen", hint: "F2", run: () => this.#startRename(n.path), off: many },
      { label: "Copy", icon: "copy", hint: "Ctrl+C", run: () => this.#copy("copy") },
      { label: "Cut", icon: "scissors", hint: "Ctrl+X", run: () => this.#copy("cut") },
    ];
    if (n.dir) items.push({ label: "Paste into folder", icon: "paste", hint: "Ctrl+V", run: () => this.#paste(n.path), off: !this.#clip });
    items.push({ sep: true }, { label: "Delete", icon: "trash", hint: "Del", danger: true, run: () => this.#trashSelected() });
    return items;
  }

  #blankMenu(): Item[] {
    const inDir = this.#data?.loc === "dir";
    return [
      { label: "Add files…", icon: "upload", run: () => send("f", "upload", this.#target()), off: !inDir },
      { label: "New folder", icon: "folderPlus", run: () => this.#newFolder(), off: !inDir },
      { sep: true },
      { label: "Paste", icon: "paste", hint: "Ctrl+V", run: () => this.#paste(this.#target()), off: !this.#clip || !inDir },
      { sep: true },
      { label: "Refresh", icon: "refresh", run: () => send("f", "refresh") },
    ];
  }

  #menu(x: number, y: number, items: Item[]): void {
    const m = this.#menuEl;
    m.replaceChildren();
    for (const it of items) {
      if (it.sep) { m.append(el("hr", "fx-menu-sep")); continue; }
      const b = el("button", it.danger ? "fx-menu-item fx-menu-danger" : "fx-menu-item");
      b.type = "button";
      b.setAttribute("role", "menuitem");
      b.disabled = !!it.off;
      if (it.icon) b.append(icon(it.icon, 16)); else b.append(el("span", "fx-menu-gap"));
      b.append(el("span", "fx-menu-label", it.label ?? ""));
      if (it.hint) b.append(el("kbd", undefined, it.hint));
      b.addEventListener("click", () => { this.#closeMenu(); it.run?.(); });
      m.append(b);
    }
    m.hidden = false;
    const box = this.getBoundingClientRect();
    m.style.left = "0px";
    m.style.top = "0px";
    const w = m.offsetWidth;
    const h = m.offsetHeight;
    m.style.left = `${Math.max(4, Math.min(x - box.left, box.width - w - 4))}px`;
    m.style.top = `${Math.max(4, Math.min(y - box.top, box.height - h - 4))}px`;
    m.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus({ preventScroll: true });
  }

  #closeMenu(): void {
    this.#menuEl.hidden = true;
  }

  // ── drag and drop ───────────────────────────────────────────────────────

  /** Marks an element as a place things can be dropped: files from inside move there, files from the computer are added. */
  #dropTarget(node: HTMLElement, dir: string | null): void {
    if (dir === null) return;
    node.addEventListener("dragover", (e) => {
      const types = e.dataTransfer?.types ?? [];
      if (!types.includes(DRAG) && !types.includes("Files")) return;
      e.preventDefault();
      e.stopPropagation();
      if (e.dataTransfer) e.dataTransfer.dropEffect = types.includes(DRAG) ? "move" : "copy";
      node.dataset.over = "true";
    });
    node.addEventListener("dragleave", () => { delete node.dataset.over; });
    node.addEventListener("drop", (e) => {
      delete node.dataset.over;
      const raw = e.dataTransfer?.getData(DRAG);
      if (raw) {
        e.preventDefault();
        e.stopPropagation();
        let paths: string[] = [];
        try { paths = (JSON.parse(raw) as unknown[]).filter((p): p is string => typeof p === "string"); } catch { return; }
        paths = paths.filter((p) => p !== dir && parentOf(p) !== dir);
        if (paths.length) send("f", "move", dir, ...paths);
        return;
      }
      if (e.dataTransfer?.files.length) {
        e.preventDefault();
        e.stopPropagation();
        void this.#upload(Array.from(e.dataTransfer.files), dir);
      }
    });
  }

  #bindDrop(): void {
    let depth = 0;
    const hasFiles = (e: DragEvent): boolean => !!e.dataTransfer?.types.includes("Files");
    this.addEventListener("dragenter", (e) => { if (hasFiles(e)) { depth++; this.#dropEl.hidden = this.#data?.loc !== "dir"; } });
    this.addEventListener("dragleave", (e) => { if (hasFiles(e) && --depth <= 0) { depth = 0; this.#dropEl.hidden = true; } });
    this.addEventListener("dragover", (e) => { if (hasFiles(e)) e.preventDefault(); });
    this.addEventListener("drop", (e) => {
      depth = 0;
      this.#dropEl.hidden = true;
      if (!hasFiles(e)) return;
      e.preventDefault();
      if (this.#data?.loc === "dir") void this.#upload(Array.from(e.dataTransfer?.files ?? []), this.#target());
    });
  }

  async #upload(files: File[], dir: string): Promise<void> {
    for (const f of files) {
      if (f.size > MAX_DROP) { this.#notify(`${f.name} is larger than 100 MB, so it was not added.`, true); continue; }
      let seq = 0;
      for (let off = 0; off < f.size || seq === 0; off += CHUNK) {
        const last = off + CHUNK >= f.size;
        const b64 = toBase64(await f.slice(off, off + CHUNK).arrayBuffer());
        send("f", "put", dir, f.name, String(seq++), last ? "1" : "0", b64);
        if (last) break;
      }
    }
  }

  // ── messages ────────────────────────────────────────────────────────────

  #notify(text: string, error = false, undo = ""): void {
    this.#showToast({ message: text, error, undo } as FilesData);
  }

  #showToast(v: FilesData): void {
    window.clearTimeout(this.#toast);
    this.#toastEl.replaceChildren();
    if (!v.message) { this.#toastEl.hidden = true; return; }
    this.#toastEl.dataset.error = String(!!v.error);
    this.#toastEl.append(el("span", undefined, v.message));
    if (v.undo) {
      const b = el("button", "btn btn-sm");
      b.type = "button";
      b.textContent = "Undo";
      b.addEventListener("click", () => { this.#toastEl.hidden = true; send("f", "restore", v.undo ?? ""); });
      this.#toastEl.append(b);
    }
    this.#toastEl.hidden = false;
    this.#toast = window.setTimeout(() => { this.#toastEl.hidden = true; }, v.undo ? 9000 : v.error ? 8000 : 4000);
  }
}

customElements.define("ph-files", PhFiles);
