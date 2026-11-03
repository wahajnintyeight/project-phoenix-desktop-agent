import { listen, send } from "./protocol";
import "./tab-strip";
import "./provider-card";
import "./model-list";
import "./quick-model";
import "./browser-settings";
import "./mcp-settings";
import "./chat";
import "./quick-actions";

const $ = (id: string): HTMLElement | null => document.getElementById(id);

// Go may only set these. Anything else is ignored.
const SETTABLE_PROPS = new Set(["tabs", "value", "disabled", "src", "hidden", "data", "result", "models", "events", "actions"]);
const SETTABLE_ATTRS = new Set(["data-view", "data-state"]);

// ── Message handling (Go → UI) ──────────────────────────────────────────
const pending = new Map<number, () => void>();

listen((op, f) => {
  switch (op) {
    case "t": { // set text: id, text
      const el = $(f[0] ?? "");
      if (el) el.textContent = f[1] ?? "";
      break;
    }
    case "u": { // set property: id, prop, json
      const el = $(f[0] ?? "");
      const prop = f[1] ?? "";
      if (!el || !SETTABLE_PROPS.has(prop)) break;
      if (prop === "value" && el === document.activeElement) break; // never clobber typing
      (el as unknown as Record<string, unknown>)[prop] = JSON.parse(f[2] ?? "null");
      break;
    }
    case "a": { // set attribute: id, name, value
      const el = $(f[0] ?? "");
      const name = f[1] ?? "";
      if (el && SETTABLE_ATTRS.has(name)) el.setAttribute(name, f[2] ?? "");
      break;
    }
    case "f": { // focus: id
      const el = $(f[0] ?? "");
      el?.focus();
      if (el instanceof HTMLInputElement) el.select();
      break;
    }
    case "s": // window state: "max" | "normal"
      document.documentElement.dataset["maximized"] = String(f[0] === "max");
      break;
    case "o": { // pong: seq
      const done = pending.get(Number(f[0]));
      if (done) { pending.delete(Number(f[0])); done(); }
      break;
    }
  }
});

// ── Events (UI → Go): delegated listeners ───────────────────────────────
document.addEventListener("click", (e) => {
  const target = (e.target as Element | null)?.closest<HTMLElement>("[data-action]");
  if (!target) return;
  const action = target.dataset["action"] ?? "";

  if (action === "probe.ping") { void runPing(); return; }
  if (action === "probe.click") { probeClick(e); return; }
  if (action === "probe.clear") { out("No results yet.", true); return; }

  send("c", action, target.dataset["id"] ?? target.id);
}, { passive: true });

const omnibox = $("omnibox") as HTMLInputElement | null;
let inputSeq = 0;
omnibox?.addEventListener("input", () => {
  send("i", "omnibox", omnibox.value, String(++inputSeq));
}, { passive: true });
omnibox?.addEventListener("keydown", (e) => {
  if (e.key !== "Enter") return;
  send("n", omnibox.value);
  omnibox.blur(); // lets Go's canonical URL replace what was typed
}, { passive: true });
omnibox?.addEventListener("focus", () => omnibox.select(), { passive: true });

// The start page's search box does what the address bar does.
const homeSearch = $("home-search") as HTMLInputElement | null;
homeSearch?.addEventListener("keydown", (e) => {
  if (e.key !== "Enter") return;
  const v = homeSearch.value.trim();
  if (!v) return;
  send("n", v);
  homeSearch.value = "";
}, { passive: true });

// Settings sections: purely local, no round trip to Go.
const settings = document.querySelector<HTMLElement>(".settings");
document.querySelector("[data-settings-nav]")?.addEventListener("click", (e) => {
  const b = (e.target as Element | null)?.closest<HTMLElement>("[data-section-btn]");
  const name = b?.dataset["sectionBtn"];
  if (!b || !name || !settings) return;
  settings.dataset["section"] = name;
  document.querySelectorAll<HTMLElement>("[data-section-btn]").forEach((other) => {
    other.setAttribute("aria-current", String(other === b));
  });
}, { passive: true });

$("menu")?.addEventListener("toggle", (e) => send("m", (e as ToggleEvent).newState === "open" ? "open" : "close"), { passive: true });

// Airspace: Go sends a snapshot of the page into #freeze, then waits for us to confirm it
// has been painted before it hides the page window.
$("freeze")?.addEventListener("load", (e) => {
  // The empty placeholder image loads at start-up; only a real snapshot is acknowledged.
  if ((e.target as HTMLImageElement).src.startsWith("data:image/gif")) return;
  requestAnimationFrame(() => requestAnimationFrame(() => send("m", "frozen")));
}, { passive: true });

// Agent panel sections: local view state, nothing for Go to know.
const agent = document.querySelector<HTMLElement>(".agent");
document.querySelector(".agent-tabs")?.addEventListener("click", (e) => {
  const b = (e.target as Element | null)?.closest<HTMLElement>("[data-pane-btn]");
  const name = b?.dataset["paneBtn"];
  if (!b || !name || !agent) return;
  agent.dataset["pane"] = name;
  document.querySelectorAll<HTMLElement>("[data-pane-btn]").forEach((t) => t.setAttribute("aria-selected", String(t === b)));
}, { passive: true });

// The start page's first step follows the quick picker: done once some provider can run.
document.addEventListener("quickchange", (e) => {
  const { ready, current } = (e as CustomEvent<{ ready: number; current: string }>).detail;
  const step = $("step-provider");
  const text = $("step-provider-text");
  if (step) step.dataset["done"] = String(ready > 0);
  if (text) {
    text.textContent = ready > 0
      ? `${current} is ready. Change it in Settings, or from the picker under the agent panel.`
      : "No provider is ready yet. Sign in with Claude or Codex, or add an API key.";
  }
});

// ── Page slot: tell Go where the page should be drawn ───────────────────
const slot = $("page-slot");
let lastSlot = "";
function reportSlot(): void {
  if (!slot) return;
  const r = slot.getBoundingClientRect();
  const key = `${r.x}|${r.y}|${r.width}|${r.height}`;
  if (key === lastSlot) return;
  lastSlot = key;
  send("l", String(r.x), String(r.y), String(r.width), String(r.height));
  const label = $("env-slot");
  if (label) label.textContent = `${Math.round(r.x)}, ${Math.round(r.y)}  ${Math.round(r.width)} × ${Math.round(r.height)}`;
}
if (slot) new ResizeObserver(reportSlot).observe(slot);
window.addEventListener("resize", reportSlot, { passive: true });

// ── Probes ──────────────────────────────────────────────────────────────
function out(text: string, replace = false): void {
  const el = $("out");
  if (!el) return;
  el.textContent = replace ? text : `${el.textContent ?? ""}\n${text}`.trimStart();
}

function pct(sorted: number[], p: number): number {
  return sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * p))] ?? 0;
}

async function runPing(): Promise<void> {
  const N = 200;
  const times: number[] = [];
  for (let seq = 1; seq <= N; seq++) {
    const t0 = performance.now();
    await new Promise<void>((resolve) => {
      pending.set(seq, resolve);
      send("p", String(seq));
    });
    times.push(performance.now() - t0);
  }
  times.sort((a, b) => a - b);
  out(`Go round trip (n=${N}): p50 ${pct(times, 0.5).toFixed(2)} ms · p95 ${pct(times, 0.95).toFixed(2)} ms · max ${(times[N - 1] ?? 0).toFixed(2)} ms`);
}

function probeClick(e: MouseEvent): void {
  const t0 = e.timeStamp;
  requestAnimationFrame(() => {
    out(`Click → next frame callback: ${(performance.now() - t0).toFixed(1)} ms (not full click-to-photon)`);
  });
}

reportSlot();
send("r", "1");
