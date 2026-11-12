import { el } from "./dom";
import { send } from "./protocol";

type Settings = { enabled: boolean; tasks: boolean; chat: boolean; approvals: boolean };
type Entry = { id: string; kind: string; title: string; at: number; read: boolean };
type State = { settings: Settings; items: Entry[] };

class PhNotifications extends HTMLElement {
  #state: State = { settings: { enabled: true, tasks: true, chat: true, approvals: true }, items: [] };
  #busy = false;
  #testPending = false;
  #testResult: { ok: boolean; message: string } | null = null;

  connectedCallback(): void {
    this.addEventListener("notification-busy", () => { this.#busy = true; this.#render(); });
    this.#render();
  }
  set data(value: State) {
    this.#state = value;
    this.#render();
    const tab = document.querySelector<HTMLElement>("[data-pane-btn=notifications]");
    const unread = value.items.filter((item) => !item.read).length;
    if (tab) {
      tab.textContent = unread ? `Notifications (${unread})` : "Notifications";
    }
    const badge = document.querySelector<HTMLElement>("#agent-details-count");
    if (badge) { badge.hidden = unread === 0; badge.textContent = String(unread); }
    document.querySelector<HTMLElement>("#agent-details")?.setAttribute("aria-label", unread ? `Agent details, ${unread} unread notifications` : "Agent details");
  }

  set result(value: { ok: boolean; message: string }) {
    this.#testPending = false;
    this.#testResult = value;
    this.#render();
  }

  #render(): void {
    const settingsOpen = this.querySelector<HTMLDetailsElement>(".notification-settings")?.open ?? false;
    this.replaceChildren();
    const header = el("div", "notification-header");
    header.append(el("h2", "text-base font-semibold", "Notifications"));
    const read = el("button", "btn btn-quiet btn-sm", "Mark all read");
    read.type = "button";
    read.addEventListener("click", () => send("j", "read-all"));
    const clear = el("button", "btn btn-quiet btn-sm", "Clear");
    clear.type = "button";
    clear.addEventListener("click", () => send("j", "clear"));
    header.append(read, clear);
    const options = el("details", "notification-settings");
    options.open = settingsOpen;
    options.append(el("summary", undefined, "Notification settings"));
    const settings: [keyof Settings, string][] = [["enabled", "Enable notifications"], ["tasks", "Tasks"], ["chat", "Agent responses"], ["approvals", "Approvals"]];
    for (const [key, label] of settings) {
      const row = el("label", "notification-option");
      const box = el("input");
      box.type = "checkbox";
      box.checked = this.#state.settings[key];
      box.disabled = key !== "enabled" && !this.#state.settings.enabled;
      box.addEventListener("change", () => send("j", "settings", JSON.stringify({ ...this.#state.settings, [key]: box.checked })));
      row.append(box, document.createTextNode(label));
      options.append(row);
    }
    const testRow = el("div", "notification-test");
    const test = el("button", "btn btn-quiet btn-sm", "Send test notification");
    test.type = "button";
    test.disabled = this.#testPending;
    test.addEventListener("click", () => {
      this.#testPending = true;
      this.#testResult = { ok: true, message: "Sending test notification…" };
      this.#render();
      send("j", "test");
    });
    testRow.append(test);
    if (this.#testResult) {
      const status = el("span", "notification-test-result", this.#testResult.message);
      status.setAttribute("role", "status");
      status.dataset["ok"] = String(this.#testResult.ok);
      testRow.append(status);
    }
    options.append(testRow);
    const list = el("div", "notification-list");
    if (this.#busy) list.append(el("p", "notification-empty", "Finish or stop the current chat before opening another conversation."));
    if (!this.#state.items.length) list.append(el("p", "notification-empty", "No notifications yet."));
    for (const item of this.#state.items) {
      const button = el("button", "notification-item");
      button.type = "button";
      button.dataset["read"] = String(item.read);
      const date = new Date(item.at);
      const time = el("time", "notification-time", Number.isNaN(date.valueOf()) ? "" : new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date));
      if (!Number.isNaN(date.valueOf())) time.dateTime = date.toISOString();
      button.append(el("span", undefined, item.title), time);
      button.addEventListener("click", () => send("j", "open", item.id));
      list.append(button);
    }
    this.append(header, options, list);
  }
}
customElements.define("ph-notifications", PhNotifications);
