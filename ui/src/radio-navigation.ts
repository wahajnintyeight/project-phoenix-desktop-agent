/** Roving keyboard navigation for button-based radio groups. */
export function handleRadioKeys(event: KeyboardEvent): void {
  if (!(event.target instanceof HTMLButtonElement) || event.target.getAttribute("role") !== "radio") return;
  const group = event.target.closest<HTMLElement>('[role="radiogroup"]');
  if (!group) return;
  const radios = Array.from(group.querySelectorAll<HTMLButtonElement>('button[role="radio"]:not(:disabled)'));
  const current = radios.indexOf(event.target);
  if (current < 0 || radios.length === 0) return;
  let next: number;
  switch (event.key) {
    case "ArrowRight": case "ArrowDown": next = (current + 1) % radios.length; break;
    case "ArrowLeft": case "ArrowUp": next = (current + radios.length - 1) % radios.length; break;
    case "Home": next = 0; break;
    case "End": next = radios.length - 1; break;
    default: return;
  }
  event.preventDefault();
  const target = radios[next];
  target?.focus();
  target?.click();
}
