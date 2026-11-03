// Wire format v0: fields separated by U+001F, records by U+001E.
//
// UI → Go:  c action id   click on a [data-action] element
//           n text        navigate (address bar submit)
//           l x y w h     page-slot rectangle in CSS pixels
//           i field value seq   input changed (type-ahead)
//           p seq         ping
//           m state       menu popover opened or closed
//           r version     UI ready
//           g send text [1|0] | act id | probe | stop | new   chat with the agent
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
  addEventListener(type: "message", listener: (e: MessageEvent<string>) => void): void;
}

declare global {
  interface Window {
    chrome: { webview: WebViewBridge };
  }
}

export function send(op: string, ...fields: string[]): void {
  window.chrome.webview.postMessage([op, ...fields].join(FS));
}

export type Handler = (op: string, fields: string[]) => void;

export function listen(handler: Handler): void {
  window.chrome.webview.addEventListener("message", (e) => {
    for (const record of e.data.split(RS)) {
      const [op = "", ...fields] = record.split(FS);
      handler(op, fields);
    }
  });
}
