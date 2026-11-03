// Tiny DOM helper shared by the custom elements: create an element with a class and text.
// Text always goes through textContent, so nothing passed in is ever parsed as HTML.
export function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string, text?: string): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}
