export interface HistoryItem {
  id: string;
  title: string;
  host: string;
}

export class PhHistoryList extends HTMLElement {
  #history: HistoryItem[] = [];

  get history(): HistoryItem[] {
    return this.#history;
  }

  set history(value: HistoryItem[]) {
    this.#history = Array.isArray(value) ? value : [];
    this.#render();
  }

  #render(): void {
    this.replaceChildren();

    const section = document.createElement("section");
    section.className = "history-section";
    section.setAttribute("aria-label", "Recently visited sites");

    const header = document.createElement("div");
    header.className = "history-heading";
    const title = document.createElement("h2");
    title.className = "text-lg";
    title.textContent = "Recently visited";
    header.append(title);

    if (this.#history.length > 0) {
      const clear = document.createElement("button");
      clear.className = "history-clear";
      clear.dataset["action"] = "history.clear";
      clear.textContent = "Clear";
      header.append(clear);
    }

    section.append(header);
    if (this.#history.length === 0) {
      const empty = document.createElement("p");
      empty.className = "history-empty";
      empty.textContent = "Sites you visit will appear here.";
      section.append(empty);
      this.append(section);
      return;
    }

    const grid = document.createElement("div");
    grid.className = "history-grid";
    for (const item of this.#history) {
      const button = document.createElement("div");
      button.className = "history-item";
      button.dataset["action"] = "history.open";
      button.dataset["id"] = item.id;
      button.title = `${item.title}\n${item.host}`;
      button.setAttribute("role", "button");
      button.tabIndex = 0;
      button.setAttribute("aria-label", `Open ${item.title} on ${item.host}`);

      const mark = document.createElement("span");
      mark.className = "history-mark";
      mark.setAttribute("aria-hidden", "true");
      mark.textContent = [...item.host][0]?.toLocaleUpperCase() ?? "W";

      const favicon = document.createElement("img");
      favicon.className = "history-favicon";
      favicon.alt = "";
      favicon.loading = "lazy";
      favicon.src = `https://${item.host}/favicon.ico`;
      favicon.addEventListener("load", () => { mark.hidden = true; });
      favicon.addEventListener("error", () => favicon.remove());

      const remove = document.createElement("button");
      remove.className = "history-remove";
      remove.dataset["action"] = "history.delete";
      remove.dataset["id"] = item.id;
      remove.type = "button";
      remove.textContent = "×";
      remove.title = `Remove ${item.host} from history`;
      remove.setAttribute("aria-label", `Remove ${item.title} from history`);

      const copy = document.createElement("span");
      copy.className = "history-copy";
      const siteTitle = document.createElement("span");
      siteTitle.className = "history-title";
      siteTitle.textContent = item.title;
      const host = document.createElement("span");
      host.className = "history-host";
      host.textContent = item.host;
      copy.append(siteTitle, host);
      button.append(mark, favicon, copy, remove);
      grid.append(button);
    }
    grid.addEventListener("keydown", (event) => {
      const target = event.target;
      if (!(target instanceof HTMLElement) || !target.matches(".history-item") || (event.key !== "Enter" && event.key !== " ")) return;
      event.preventDefault();
      target.click();
    });
    section.append(grid);
    this.append(section);
  }
}

customElements.define("ph-history-list", PhHistoryList);
