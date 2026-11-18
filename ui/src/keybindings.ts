import { el } from "./dom";
import { send } from "./protocol";

const actions = [
  { id: "new", label: "New tab", selector: '[data-action="tab.new"]', keys: "T" },
  { id: "back", label: "Back", selector: "#nav-back", keys: "B" },
  { id: "forward", label: "Forward", selector: "#nav-forward", keys: "F" },
  { id: "reload", label: "Reload", selector: "#nav-reload", keys: "R" },
  { id: "settings", label: "Settings", selector: '.rail-settings', keys: "S" },
  { id: "browser", label: "Browser", selector: '[data-rail="browser"]', keys: "W" },
  { id: "files", label: "Files", selector: '[data-rail="files"]', keys: "L" },
  { id: "tasks", label: "Tasks", selector: '[data-rail="tasks"]', keys: "K" },
  { id: "voice", label: "Dictate", selector: '.composer .voice-button', keys: "V" },
];
const tipKeys = "ABCEFGHIJKLMNOPQRSTUVWXYZ,./;[]-='!@#$%^&*()_+{}:<>?";
const storageKey = "phoenix.keybindings.alt";
let saved: Record<string, string[]> = {};
try {
  const parsed: unknown = JSON.parse(localStorage.getItem(storageKey) || "{}");
  if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) saved = parsed as Record<string, string[]>;
} catch { /* use defaults */ }
function keysFor(action: typeof actions[number]): string[] {
  const value = saved[action.id];
  return Array.isArray(value) ? value.filter((key) => typeof key === "string" && key.length === 1 && tipKeys.includes(key)) : [action.keys];
}
function renderSettings(): void {
  const container = document.querySelector<HTMLElement>("#keybindings-settings");
  if (!container) return;
  container.replaceChildren();
  for (const action of actions) {
    const row = el("label", "keybindings-edit-row");
    const input = el("input", "keybindings-edit-input");
    input.type = "text";
    input.value = keysFor(action).join(" ");
    input.setAttribute("aria-label", `Alt shortcuts for ${action.label}`);
    input.addEventListener("change", () => {
      const keys = input.value.toUpperCase().trim().split(/\s+/).filter(Boolean);
      const used = new Set(actions.filter((other) => other !== action).flatMap(keysFor));
      const status = document.querySelector<HTMLElement>("#keybindings-status");
      if (!keys.length || keys.some((key) => key.length !== 1 || !tipKeys.includes(key) || used.has(key)) || new Set(keys).size !== keys.length) {
        if (status) status.textContent = "Use unique single characters. Alt+D is reserved for the address bar.";
        input.value = keysFor(action).join(" ");
        return;
      }
      const updated = { ...saved, [action.id]: keys };
      try { localStorage.setItem(storageKey, JSON.stringify(updated)); saved = updated; if (status) status.textContent = `${action.label} shortcuts saved.`; }
      catch { if (status) status.textContent = "Could not save shortcuts."; input.value = keysFor(action).join(" "); }
    });
    row.append(el("span", undefined, action.label), input);
    container.append(row);
  }
}
renderSettings();

const bindings = [
  ["New tab", "Ctrl+T"], ["Close tab", "Ctrl+W / Ctrl+F4"],
  ["Next / previous tab", "Ctrl+Tab / Ctrl+Shift+Tab"], ["Select tab", "Ctrl+1–9"],
  ["Address bar", "Ctrl+L / Alt+D / F6"], ["Back / forward", "Alt+Left / Alt+Right"],
  ["Reload", "Ctrl+R / F5"], ["Settings", "Ctrl+,"], ["Developer tools", "F12 / Ctrl+Shift+I"],
  ["Dictate", "Ctrl+Shift+V"], ["Cancel recording", "Escape"], ["Send chat message", "Enter"],
  ["New line in message", "Shift+Enter"],
];

const dialog = el("dialog", "keybindings-dialog");
dialog.setAttribute("aria-labelledby", "keybindings-title");
const heading = el("h2", "keybindings-title", "Keyboard shortcuts");
heading.id = "keybindings-title";
const close = el("button", "btn btn-sm", "Close");
close.type = "button";
close.addEventListener("click", () => dialog.close());
const list = el("dl", "keybindings-list");
for (const [name, key] of bindings) {
  const row = el("div", "keybindings-row");
  row.append(el("dt", undefined, name), el("dd", undefined, key));
  list.append(row);
}
const manage = el("button", "btn btn-sm", "Edit keybindings");
manage.type = "button";
manage.addEventListener("click", () => { dialog.close(); send("c", "menu.settings", ""); document.querySelector<HTMLButtonElement>('[data-section-btn="keybindings"]')?.click(); });
dialog.append(heading, el("p", "keybindings-note", "Hold Alt to show shortcuts for visible buttons, then press a character. Edit shortcuts in Settings."), list, manage, close);
document.body.append(dialog);
document.querySelector<HTMLButtonElement>("#keybindings-open")?.addEventListener("click", () => {
  document.querySelector<HTMLElement>("#menu")?.hidePopover();
  dialog.showModal();
});

let targets: HTMLButtonElement[] = [];
let badges: HTMLElement[] = [];
let assigned = new Map<string, HTMLButtonElement>();
function clearTips(): void {
  badges.forEach((badge) => badge.remove());
  badges = [];
  targets = [];
  assigned.clear();
}
function showTips(): void {
  clearTips();
  targets = Array.from(document.querySelectorAll<HTMLButtonElement>("button")).filter((button) => {
    const rect = button.getBoundingClientRect();
    return !button.disabled && rect.width > 0 && rect.height > 0 && rect.bottom > 0 && rect.right > 0 && rect.top < innerHeight && rect.left < innerWidth && getComputedStyle(button).visibility === "visible";
  });
  const reserved = new Set(actions.flatMap(keysFor));
  const available = Array.from(tipKeys).filter((key) => !reserved.has(key));
  targets.forEach((button) => {
    const action = actions.find((item) => button.matches(item.selector));
    const keys = action ? keysFor(action) : [available.shift() ?? ""];
    const key = keys[0];
    if (!key || assigned.has(key)) return;
    for (const binding of keys) if (!assigned.has(binding)) assigned.set(binding, button);
    const rect = button.getBoundingClientRect();
    const badge = el("span", "keytip", key);
    badge.setAttribute("aria-hidden", "true");
    badge.style.left = `${Math.max(0, Math.min(innerWidth - 28, rect.left))}px`;
    badge.style.top = `${Math.max(0, rect.top)}px`;
    document.body.append(badge);
    badges.push(badge);
  });
}
document.addEventListener("keydown", (event) => {
  if (event.key === "Alt" && !event.repeat && !event.ctrlKey) { showTips(); return; }
  const key = event.key.toUpperCase();
  if (!event.altKey || event.ctrlKey || event.repeat || !assigned.has(key)) return;
  event.preventDefault();
  const target = assigned.get(key);
  clearTips();
  if (target?.isConnected && !target.disabled) target.click();
});
document.addEventListener("keyup", (event) => { if (event.key === "Alt") clearTips(); });
window.addEventListener("blur", clearTips);
window.addEventListener("resize", clearTips);
document.addEventListener("scroll", clearTips, true);
