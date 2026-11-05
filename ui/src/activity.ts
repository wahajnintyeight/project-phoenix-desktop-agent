import { el } from "./dom";
import { send } from "./protocol";

export interface ActivityEntry {
  at: string;
  kind: "tool" | "approval" | "turn";
  summary: string;
  status?: "started" | "completed" | "stopped" | "failed" | "pending" | "allowed" | "declined";
  ms?: number;
}

/** Persistent, deliberately minimal summaries of what the agent did. */
class PhActivity extends HTMLElement {
  readonly #list = el("div", "activity-list");
  readonly #empty = el("div", "activity-empty");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    const clear = el("button", "btn btn-quiet btn-sm", "Clear saved chats & activity");
    clear.type = "button";
    clear.addEventListener("click", () => {
      if (window.confirm("Delete all saved chats and agent activity from this device?")) {
        send("g", "clear-history");
      }
    });

    this.#empty.append(
      el("h2", undefined, "No agent activity yet"),
      el("p", undefined, "Tool use, permission decisions, and run outcomes will appear here. Phoenix stores summaries only."),
    );
    const header = el("div", "activity-head");
    header.append(el("h2", undefined, "Agent activity"), clear);
    this.append(header, this.#empty, this.#list);
  }

  set activity(entries: ActivityEntry[]) {
    this.#list.replaceChildren(...entries.map((entry) => this.#row(entry)));
    const empty = entries.length === 0;
    this.#empty.hidden = !empty;
    this.#list.hidden = empty;
  }

  #row(entry: ActivityEntry): HTMLElement {
    const row = el("article", "activity-row");
    if (entry.status) row.dataset["status"] = entry.status;
    const top = el("div", "activity-row-top");
    top.append(el("span", "activity-dot"));
    const summary = el("p", "activity-summary", entry.summary);
    const at = new Date(entry.at);
    const time = el("time", "activity-time", Number.isNaN(at.valueOf())
      ? ""
      : new Intl.DateTimeFormat(undefined, { dateStyle: "short", timeStyle: "short" }).format(at));
    if (!Number.isNaN(at.valueOf())) time.dateTime = at.toISOString();
    top.append(summary, time);
    row.append(top);
    const detail = [this.#status(entry.status), entry.ms ? `${(entry.ms / 1000).toFixed(1)} s` : ""].filter(Boolean).join(" · ");
    if (detail) row.append(el("p", "activity-detail", detail));
    return row;
  }

  #status(status?: ActivityEntry["status"]): string {
    switch (status) {
      case "started": return "Started";
      case "completed": return "Completed";
      case "stopped": return "Stopped";
      case "failed": return "Failed";
      case "pending": return "Awaiting your decision";
      case "allowed": return "Allowed";
      case "declined": return "Declined";
      default: return "";
    }
  }
}

customElements.define("ph-activity", PhActivity);
