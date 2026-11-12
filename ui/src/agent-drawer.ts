import { send } from "./protocol";

const STORAGE_KEY = "phoenix.agentDrawer";

export function initAgentDrawer(): void {
  const workspace = document.querySelector<HTMLElement>("#workspace");
  const agent = document.querySelector<HTMLElement>("#agent-panel");
  const quickActions = document.querySelector<HTMLElement>("#quick-actions");
  const mode = document.querySelector<HTMLButtonElement>("#agent-mode");
  const collapse = document.querySelector<HTMLButtonElement>("#agent-collapse");
  const details = document.querySelector<HTMLButtonElement>("#agent-details");
  const handle = document.querySelector<HTMLButtonElement>("#agent-drag");
  const edges = document.querySelectorAll<HTMLElement>("[data-drawer-edge]");
  if (!workspace || !agent || !mode || !collapse || !handle) return;

  let height = 0;
  let expandedHeight = 0;
  let preferredWidth = 0;
  let width = 0;
  let collapsed = false;
  let drawerMode = false;
  let overlaySent = false;
  let ready = false;
  let boundsFrame = 0;
  let lastBounds = "";
  let drag: { pointer: number; x: number; y: number; height: number; width: number; edge: string } | null = null;
  const min = 44;
  const max = (): number => Math.max(min, workspace.clientHeight - (quickActions?.offsetHeight ?? 56) - 24);
  const maxWidth = (): number => Math.max(200, workspace.clientWidth - 92);
  const reportBounds = (): void => {
    if (!ready || boundsFrame) return;
    boundsFrame = requestAnimationFrame(() => {
      boundsFrame = 0;
      const r = agent.getBoundingClientRect();
      const fields = drawerMode && !collapsed ? [r.x, r.y, r.width, r.height] : [0, 0, 0, 0];
      const value = fields.map((v) => String(Math.round(v * 100) / 100));
      const key = value.join("|");
      if (key !== lastBounds) { lastBounds = key; send("d", ...value); }
    });
  };
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null");
    if (saved && typeof saved === "object") {
      const state = saved as Record<string, unknown>;
      if (typeof state.height === "number" && Number.isFinite(state.height) && state.height > min && state.height <= 10000) expandedHeight = state.height;
      if (typeof state.width === "number" && Number.isFinite(state.width) && state.width >= 200 && state.width <= 10000) preferredWidth = state.width;
      collapsed = state.collapsed === true;
      drawerMode = state.mode === "drawer";
    }
  } catch { /* Preferences remain available for this session. */ }
  const save = (): void => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify({ mode: drawerMode ? "drawer" : "side", height: expandedHeight, width: preferredWidth, collapsed })); }
    catch { /* Preferences remain available for this session. */ }
  };
  const syncOverlay = (): void => {
    const open = drawerMode && !collapsed;
    reportBounds();
    if (!ready || open === overlaySent) return;
    overlaySent = open;
    send("m", open ? "drawer-open" : "drawer-close");
  };
  const applyWidth = (value: number): void => {
    width = Math.round(Math.max(Math.min(320, maxWidth()), Math.min(maxWidth(), value)));
    workspace.style.setProperty("--drawer-width", `${width}px`);
    reportBounds();
    edges.forEach((edge) => {
      edge.setAttribute("aria-valuemin", String(Math.min(320, maxWidth())));
      edge.setAttribute("aria-valuemax", String(maxWidth()));
      edge.setAttribute("aria-valuenow", String(width));
    });
  };
  const apply = (value: number): void => {
    height = Math.max(min, Math.min(max(), Math.round(value)));
    workspace.style.setProperty("--drawer-height", `${height}px`);
    collapsed = height <= min;
    workspace.dataset["drawerCollapsed"] = String(collapsed);
    collapse.textContent = collapsed ? "Restore" : "Minimize";
    collapse.setAttribute("aria-label", collapsed ? "Restore agent drawer" : "Minimize agent drawer");
    if (details) details.disabled = collapsed;
    agent.querySelectorAll<HTMLElement>("ph-context, .agent-tabs, .agent-body").forEach((element) => { element.inert = collapsed; });
    syncOverlay();
  };
  const setMode = (drawer: boolean): void => {
    drawerMode = drawer;
    workspace.dataset["agentMode"] = drawer ? "drawer" : "side";
    mode.textContent = drawer ? "Side panel" : "Expand";
    mode.setAttribute("aria-label", drawer ? "Move agent to side panel" : "Move agent to bottom drawer");
    mode.title = drawer ? "Move agent to side panel" : "Move agent to bottom drawer";
    if (drawer) {
      if (!expandedHeight) expandedHeight = max();
      if (!preferredWidth) preferredWidth = Math.min(900, maxWidth());
      applyWidth(preferredWidth);
      apply(collapsed ? min : expandedHeight);
    }
    else {
      workspace.dataset["drawerCollapsed"] = "false";
      if (details) details.disabled = false;
      agent.querySelectorAll<HTMLElement>("ph-context, .agent-tabs, .agent-body").forEach((element) => { element.inert = false; });
      syncOverlay();
    }
  };
  setMode(drawerMode);
  requestAnimationFrame(() => { ready = true; syncOverlay(); });
  new ResizeObserver(reportBounds).observe(agent);
  window.addEventListener("resize", reportBounds, { passive: true });
  mode.addEventListener("click", () => { setMode(!drawerMode); save(); });
  collapse.addEventListener("click", () => {
    if (height <= min) apply(expandedHeight || max());
    else { expandedHeight = height; apply(min); }
    save();
  });
  handle.addEventListener("pointerdown", (event) => {
    if (event.button !== 0) return;
    event.preventDefault();
    drag = { pointer: event.pointerId, x: event.clientX, y: event.clientY, height, width, edge: "top" };
    handle.setPointerCapture(event.pointerId);
    workspace.dataset["draggingDrawer"] = "true";
  });
  handle.addEventListener("pointermove", (event) => {
    if (drag && drag.pointer === event.pointerId) apply(drag.height + drag.y - event.clientY);
  });
  const finish = (): void => {
    if (!drag) return;
    drag = null;
    delete workspace.dataset["draggingDrawer"];
    if (height > min) expandedHeight = height;
    save();
  };
  handle.addEventListener("pointerup", finish);
  handle.addEventListener("pointercancel", finish);
  handle.addEventListener("lostpointercapture", finish);
  edges.forEach((edge) => {
    edge.addEventListener("pointerdown", (event) => {
      if (event.button !== 0) return;
      event.preventDefault();
      drag = { pointer: event.pointerId, x: event.clientX, y: event.clientY, height, width, edge: edge.dataset["drawerEdge"] ?? "right" };
      edge.setPointerCapture(event.pointerId);
      workspace.dataset["draggingDrawer"] = "true";
    });
    edge.addEventListener("pointermove", (event) => {
      if (!drag || event.pointerId !== drag.pointer) return;
      applyWidth(drag.width + (event.clientX - drag.x) * (drag.edge === "left" ? -2 : 2));
    });
    const finishEdge = (): void => { if (drag) { preferredWidth = width; finish(); } };
    edge.addEventListener("pointerup", finishEdge);
    edge.addEventListener("pointercancel", finishEdge);
    edge.addEventListener("lostpointercapture", finishEdge);
    edge.addEventListener("keydown", (event) => {
      const delta = event.key === "ArrowRight" ? 32 : event.key === "ArrowLeft" ? -32 : 0;
      if (!delta) return;
      event.preventDefault();
      applyWidth(width + delta * (edge.dataset["drawerEdge"] === "left" ? -1 : 1));
      preferredWidth = width;
      save();
    });
  });
  handle.addEventListener("keydown", (event) => {
    const next = event.key === "ArrowUp" ? height + 32 : event.key === "ArrowDown" ? height - 32
      : event.key === "Home" ? min : event.key === "End" ? max() : null;
    if (next === null) return;
    event.preventDefault();
    apply(next);
    if (height > min) expandedHeight = height;
    save();
  });
  const resize = (): void => {
    workspace.style.setProperty("--quick-actions-height", `${quickActions?.offsetHeight ?? 56}px`);
    if (drawerMode && !drag) { applyWidth(preferredWidth || maxWidth()); apply(collapsed ? min : expandedHeight || max()); }
  };
  const observer = new ResizeObserver(resize);
  observer.observe(workspace);
  if (quickActions) observer.observe(quickActions);
  resize();
}
