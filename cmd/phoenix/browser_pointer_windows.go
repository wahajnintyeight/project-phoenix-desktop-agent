package main

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"
)

type browserPointerPosition struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Action string  `json:"action"`
}

type browserPagePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

const browserPointerScript = `(()=>{
  const a=__PHOENIX_POINTER_ARGS__;
  let h=window.__phoenixDesktopAgentPointer;
  const fresh=!(h instanceof HTMLElement&&h.isConnected&&h.shadowRoot);
  if(fresh){
    h=document.createElement("div");
    h.setAttribute("aria-hidden","true");
    h.inert=true;
    h.style.cssText="position:fixed;inset:0;z-index:2147483647;pointer-events:none";
    const r=h.attachShadow({mode:"open"});
    const s=document.createElement("style");
    s.textContent=[
      ":host{all:initial}*{box-sizing:border-box}",
      ".pointer{position:absolute;left:0;top:0;width:24px;height:32px;pointer-events:none;opacity:0;transition:transform 120ms cubic-bezier(.16,1,.3,1),opacity 100ms linear}",
      ".cursor{position:relative;z-index:1;display:block;width:24px;height:32px}.cursor path{fill:#ff9a52;stroke:#101316;stroke-width:2.2;stroke-linejoin:round}",
      ".ring{position:absolute;z-index:0;left:0;top:0;width:22px;height:22px;border:1.5px solid #ff9a52;border-radius:50%;opacity:0;pointer-events:none}",
      ".ring.active{animation:pulse 280ms ease-out both}",
      "@keyframes pulse{0%{opacity:.8;transform:scale(.45)}100%{opacity:0;transform:scale(1.35)}}",
      "@media(prefers-reduced-motion:reduce){.pointer{transition:opacity 100ms linear}.ring.active{animation:none;opacity:.75}}",
    ].join("");
    const p=document.createElement("div");
    p.classList.add("pointer");
    const svg=document.createElementNS("http://www.w3.org/2000/svg","svg");
    svg.classList.add("cursor");
    svg.setAttribute("viewBox","0 0 24 32");
    svg.setAttribute("aria-hidden","true");
    const path=document.createElementNS("http://www.w3.org/2000/svg","path");
    path.setAttribute("d","M2 1.5v22l6-6 4.6 10 3.2-1.5-4.6-9.8 8.5-.3z");
    svg.append(path);
    const ring=document.createElement("span");
    ring.className="ring";
    p.append(svg,ring);
    r.append(s,p);
    h.__phoenixPointer=p;
    h.__phoenixRing=ring;
    Object.defineProperty(window,"__phoenixDesktopAgentPointer",{value:h,writable:true,configurable:true});
    (document.body||document.documentElement).append(h);
  }
  const p=h.__phoenixPointer;
  const ring=h.__phoenixRing;
  clearTimeout(h.__phoenixTimer);
  p.style.transform="translate3d("+a.x+"px,"+a.y+"px,0)";
  p.style.willChange="transform";
  p.style.opacity="1";
  if(["click","drag","scroll","select","press","copy","paste"].includes(a.action)){
    ring.classList.remove("active");
    void ring.offsetWidth;
    ring.classList.add("active");
  }
  if(fresh)requestAnimationFrame(()=>{p.style.transition="transform 120ms cubic-bezier(.16,1,.3,1),opacity 100ms linear"});
  h.__phoenixTimer=setTimeout(()=>{if(p.isConnected){p.style.opacity="0";p.style.willChange="auto"}},950);
  return true;
})()`

// showBrowserAgentPointer draws an ephemeral cursor inside the actual page. Browser input is
// dispatched through CDP, which moves the page's input target without moving the Windows cursor.
func showBrowserAgentPointer(ctx context.Context, state browserTabState, x, y float64, action string) bool {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return false
	}
	args, err := json.Marshal(browserPointerPosition{X: x, Y: y, Action: action})
	if err != nil {
		return false
	}
	expression := strings.Replace(browserPointerScript, "__PHOENIX_POINTER_ARGS__", string(args), 1)
	_, _, err = cdpCall(ctx, state.ID, state.Epoch, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true})
	return err == nil
}

// Give the cursor one short movement beat before the matching input event is sent.
func browserPointerBeat(ctx context.Context) error {
	timer := time.NewTimer(120 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// browserFocusedPoint locates the current selection or focused control without reading its
// text, so keyboard and clipboard operations can be shown at their actual page target.
func browserFocusedPoint(ctx context.Context, state browserTabState) (browserPagePoint, bool) {
	const expression = `(()=>{let r=null;const s=getSelection();if(s&&s.rangeCount&&s.toString()){const q=s.getRangeAt(0).getBoundingClientRect();if(q.width&&q.height)r=q}if(!r){const e=document.activeElement;if(e&&e!==document.body&&e!==document.documentElement)r=e.getBoundingClientRect()}if(!r||!r.width||!r.height)return null;return{x:r.left+r.width/2,y:r.top+r.height/2}})()`
	var result struct {
		Result struct {
			Value *browserPagePoint `json:"value"`
		} `json:"result"`
	}
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true}, &result); err != nil || result.Result.Value == nil {
		return browserPagePoint{}, false
	}
	return *result.Result.Value, true
}
