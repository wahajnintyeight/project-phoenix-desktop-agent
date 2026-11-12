const STORAGE_KEY = "phoenix.agentDrawer";

export function initAgentDrawer(): void {
  const workspace = document.querySelector<HTMLElement>("#workspace");
  const agent = document.querySelector<HTMLElement>("#agent-panel");
  const mode = document.querySelector<HTMLButtonElement>("#agent-mode");
  const collapse = document.querySelector<HTMLButtonElement>("#agent-collapse");
  const handle = document.querySelector<HTMLButtonElement>("#agent-drag");
  if (!workspace || !agent || !mode || !collapse || !handle) return;

  let height = 0;
  let expandedHeight = 0;
  let collapsed = false;
  let drawerMode = false;
  let drag: { pointer: number; y: number; height: number } | null = null;
  const min = 44;
  const max = (): number => Math.max(min, workspace.clientHeight);
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null");
    if (saved && typeof saved === "object") {
      const state = saved as Record<string, unknown>;
      if (typeof state.height === "number" && Number.isFinite(state.height) && state.height > min && state.height <= 10000) expandedHeight = state.height;
      collapsed = state.collapsed === true;
      drawerMode = state.mode === "drawer";
    }
  } catch { /* Preferences remain available for this session. */ }
  const save = (): void => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify({ mode: drawerMode ? "drawer" : "side", height: expandedHeight, collapsed })); }
    catch { /* Preferences remain available for this session. */ }
  };
  const apply = (value: number): void => {
    height = Math.max(min, Math.min(max(), Math.round(value)));
    workspace.style.setProperty("--drawer-height", `${height}px`);
    collapsed = height <= min;
    workspace.dataset["drawerCollapsed"] = String(collapsed);
    collapse.textContent = collapsed ? "Restore" : "Minimize";
    collapse.setAttribute("aria-label", collapsed ? "Restore agent drawer" : "Minimize agent drawer");
    agent.querySelectorAll<HTMLElement>("ph-context, .agent-tabs, .agent-body").forEach((element) => { element.inert = collapsed; });
  };
  const setMode = (drawer: boolean): void => {
    drawerMode = drawer;
    workspace.dataset["agentMode"] = drawer ? "drawer" : "side";
    mode.textContent = drawer ? "Side panel" : "Expand";
    mode.setAttribute("aria-label", drawer ? "Move agent to side panel" : "Move agent to bottom drawer");
    mode.title = drawer ? "Move agent to side panel" : "Move agent to bottom drawer";
    if (drawer) {
      if (!expandedHeight) expandedHeight = max();
      apply(collapsed ? min : expandedHeight);
    }
    else {
      workspace.dataset["drawerCollapsed"] = "false";
      agent.querySelectorAll<HTMLElement>("ph-context, .agent-tabs, .agent-body").forEach((element) => { element.inert = false; });
    }
  };
  setMode(drawerMode);
  mode.addEventListener("click", () => { setMode(!drawerMode); save(); });
  collapse.addEventListener("click", () => {
    if (height <= min) apply(expandedHeight || max());
    else { expandedHeight = height; apply(min); }
    save();
  });
  handle.addEventListener("pointerdown", (event) => {
    if (event.button !== 0) return;
    event.preventDefault();
    drag = { pointer: event.pointerId, y: event.clientY, height };
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
  handle.addEventListener("keydown", (event) => {
    const next = event.key === "ArrowUp" ? height + 32 : event.key === "ArrowDown" ? height - 32
      : event.key === "Home" ? min : event.key === "End" ? max() : null;
    if (next === null) return;
    event.preventDefault();
    apply(next);
    if (height > min) expandedHeight = height;
    save();
  });
  new ResizeObserver(() => { if (drawerMode && !drag) apply(collapsed ? min : expandedHeight || max()); }).observe(workspace);
}
