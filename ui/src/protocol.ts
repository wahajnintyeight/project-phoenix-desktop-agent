// Wire format v0: fields separated by U+001F, records by U+001E.
//
// UI → Go:  c action id   click on a [data-action] element
//           n text        navigate (address bar submit)
//           b url         open a Markdown link in a new in-app browser tab
//           l x y w h     page-slot rectangle in CSS pixels
//           i field value seq   input changed (type-ahead)
//           p seq         ping
//           m state       menu popover opened or closed
//           r version     UI ready
//           g send text [1|0] | act id | probe | stop | new | restore id | clear-history
//           k refresh | save id name prompt kind every at | delete id | enable id 0|1 | run id | stop   saved tasks
//           v sub id [a] [b]  provider settings: refresh, mode, key, clearkey, default,
//                          login, test, model <id> <model> <effort>, search <url>
// Go → UI:  u id prop json   set an allow-listed property on an element
//           a id attr value  set an allow-listed attribute
//           t id text        set text content
//           f id             focus (and select) an element
//           s state          window state: max | normal
//           o seq            pong

const FS = "\u001f";
const RS = "\u001e";

interface WebViewBridge {
  postMessage(message: string): void;
}

declare global {
  interface Window {
    chrome: { webview: WebViewBridge };
    /** Called by the Wails host (WebviewWindow.ExecJS) with one batch of records. */
    __phx?: (batch: string) => void;
  }
}

/** UI to Go. Wails delivers the string to the application's RawMessageHandler. */
export function send(op: string, ...fields: string[]): void {
  window.chrome.webview.postMessage([op, ...fields].join(FS));
}

export type Handler = (op: string, fields: string[]) => void;

/** Go to UI. The host calls `window.__phx(batch)`; records are split here. */
export function listen(handler: Handler): void {
  window.__phx = (batch: string) => {
    for (const record of batch.split(RS)) {
      const [op = "", ...fields] = record.split(FS);
      handler(op, fields);
    }
  };
}
