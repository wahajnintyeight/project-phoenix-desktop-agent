/** Resizable assistant panel. Width is in CSS pixels, independent of page zoom. */
const MIN_WIDTH = 300;
const MAX_WIDTH = 840;
const DEFAULT_WIDTH = 380;
const PAGE_MIN_WIDTH = 320;
const RAIL_AND_DIVIDER = 60;
const STORAGE_KEY = "phoenix.agentPanelWidth";

export function initPanelResize(): void {
  const workspace = document.querySelector<HTMLElement>("#workspace");
  const handle = document.querySelector<HTMLElement>("#agent-resize");
  if (!workspace || !handle) return;

  let preferred = DEFAULT_WIDTH;
  try {
    const stored = Number(localStorage.getItem(STORAGE_KEY));
    if (Number.isFinite(stored) && stored >= MIN_WIDTH && stored <= MAX_WIDTH) preferred = stored;
  } catch { /* The panel still resizes for this session. */ }
  let width = DEFAULT_WIDTH;
  let drag: { pointer: number; x: number; width: number } | null = null;
  let frame = 0;

  const maxWidth = (): number => Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, workspace.clientWidth - RAIL_AND_DIVIDER - PAGE_MIN_WIDTH));
  const apply = (value: number): void => {
    width = Math.round(Math.max(MIN_WIDTH, Math.min(maxWidth(), value)));
    workspace.style.setProperty("--agent-width", `${width}px`);
    handle.setAttribute("aria-valuemin", String(MIN_WIDTH));
    handle.setAttribute("aria-valuemax", String(maxWidth()));
    handle.setAttribute("aria-valuenow", String(width));
    handle.setAttribute("aria-valuetext", `${width} pixels`);
  };
  const save = (): void => {
    try { localStorage.setItem(STORAGE_KEY, String(preferred)); } catch { /* Session only. */ }
  };
  const finish = (): void => {
    if (!drag) return;
    if (frame) cancelAnimationFrame(frame);
    frame = 0;
    apply(preferred);
    preferred = width;
    drag = null;
    document.documentElement.removeAttribute("data-resizing-panel");
    save();
  };

  handle.addEventListener("pointerdown", (event) => {
    if (event.button !== 0) return;
    event.preventDefault();
    drag = { pointer: event.pointerId, x: event.clientX, width };
    handle.setPointerCapture(event.pointerId);
    document.documentElement.dataset["resizingPanel"] = "true";
  });
  handle.addEventListener("pointermove", (event) => {
    if (!drag || event.pointerId !== drag.pointer) return;
    preferred = drag.width + drag.x - event.clientX;
    if (!frame) frame = requestAnimationFrame(() => { frame = 0; apply(preferred); });
  });
  handle.addEventListener("pointerup", finish);
  handle.addEventListener("pointercancel", finish);
  handle.addEventListener("lostpointercapture", finish);
  handle.addEventListener("dblclick", () => { preferred = DEFAULT_WIDTH; apply(preferred); save(); });
  handle.addEventListener("keydown", (event) => {
    const step = event.shiftKey ? 64 : 16;
    const next = event.key === "ArrowLeft" ? width + step : event.key === "ArrowRight" ? width - step
      : event.key === "Home" ? MIN_WIDTH : event.key === "End" ? maxWidth() : null;
    if (next === null) return;
    event.preventDefault();
    apply(next);
    preferred = width;
    save();
  });
  new ResizeObserver(() => apply(preferred)).observe(workspace);
  apply(preferred);
}
