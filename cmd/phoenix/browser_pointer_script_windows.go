package main

// browserPointerScript is the on-page overlay that makes the agent's actions visible: a glowing
// cursor with a comet trail, a lock-on halo and light sweep around the target, ripples, an action
// chip, an edge wash while scrolling and a faint rim light while the agent is acting.
//
// It is a constant: the only values placed in it are numbers and a short label chosen by Go
// (never page text or model text). It draws inside a closed-off shadow root with pointer events
// off, reads only element geometry, and respects prefers-reduced-motion. Edit pointer.js-style
// changes here, then check them on a real page.
const browserPointerScript = `(()=>{
  const a=__PHOENIX_POINTER_ARGS__;
  const reduce=matchMedia("(prefers-reduced-motion: reduce)").matches;
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
      ".rim{position:absolute;inset:0;opacity:0;transition:opacity 280ms ease;box-shadow:inset 0 0 0 1px rgba(173,133,155,.35),inset 0 0 44px 2px rgba(173,133,155,.15)}.rim.on{opacity:1}",
      ".pointer{position:absolute;left:0;top:0;width:24px;height:32px;opacity:0;transition:transform 260ms cubic-bezier(.22,1,.36,1),opacity 180ms linear;filter:drop-shadow(0 0 6px rgba(173,133,155,.85)) drop-shadow(0 0 16px rgba(173,133,155,.4))}",
      ".cursor{display:block;width:24px;height:32px}.cursor path{fill:#ad859b;stroke:#150e12;stroke-width:2.2;stroke-linejoin:round}",
      ".fx{position:absolute;left:0;top:0;pointer-events:none}",
      ".ripple{width:14px;height:14px;margin:-7px 0 0 -7px;border-radius:50%;border:2px solid #ebe0e6;box-shadow:0 0 14px 2px rgba(173,133,155,.85);background:radial-gradient(circle,rgba(173,133,155,.6),rgba(173,133,155,0) 70%);animation:ripple 620ms cubic-bezier(.16,1,.3,1) both}",
      ".ripple.b{animation-delay:110ms;animation-duration:720ms;border-width:1px}",
      "@keyframes ripple{0%{opacity:1;transform:scale(.4)}100%{opacity:0;transform:scale(5)}}",
      ".target{box-shadow:0 0 0 2px rgba(173,133,155,.95),0 0 22px 3px rgba(173,133,155,.5),inset 0 0 14px rgba(173,133,155,.22);overflow:hidden;animation:lock var(--d,1100ms) cubic-bezier(.16,1,.3,1) both}",
      ".target::after{content:'';position:absolute;inset:0;background:linear-gradient(105deg,transparent 30%,rgba(235,224,230,.6) 50%,transparent 70%);transform:translateX(-110%);animation:sweep 760ms 80ms ease-out both}",
      "@keyframes lock{0%{opacity:0;transform:scale(1.12)}14%{opacity:1;transform:scale(1)}72%{opacity:1}100%{opacity:0}}",
      "@keyframes sweep{to{transform:translateX(110%)}}",
      ".chip{font:600 11px/1 'Segoe UI Variable Text','Segoe UI',system-ui,sans-serif;color:#150e12;background:#ad859b;padding:4px 7px;border-radius:6px;white-space:nowrap;box-shadow:0 2px 12px rgba(173,133,155,.5);animation:chip var(--d,1100ms) cubic-bezier(.16,1,.3,1) both}",
      "@keyframes chip{0%{opacity:0;transform:translateY(4px)}14%{opacity:1;transform:none}74%{opacity:1}100%{opacity:0}}",
      ".trail{height:3px;margin-top:-1.5px;border-radius:3px;transform-origin:0 50%;background:linear-gradient(90deg,rgba(173,133,155,0),rgba(173,133,155,.95));box-shadow:0 0 10px rgba(173,133,155,.7);animation:fade 560ms ease-out both}",
      "@keyframes fade{0%{opacity:.95}100%{opacity:0}}",
      ".wash{background:linear-gradient(var(--g),rgba(173,133,155,.34),rgba(173,133,155,0));animation:wash 680ms ease-out both}",
      "@keyframes wash{0%{opacity:0}30%{opacity:1}100%{opacity:0}}",
      "@media(prefers-reduced-motion:reduce){.pointer{transition:opacity 180ms linear}.ripple,.trail,.wash{display:none}.target,.chip{animation:none;opacity:1}.target::after{display:none}}",
    ].join("");
    const rim=document.createElement("div");rim.className="rim";
    const layer=document.createElement("div");layer.className="layer";
    const p=document.createElement("div");p.className="pointer";
    const svg=document.createElementNS("http://www.w3.org/2000/svg","svg");
    svg.setAttribute("class","cursor");svg.setAttribute("viewBox","0 0 24 32");svg.setAttribute("aria-hidden","true");
    const path=document.createElementNS("http://www.w3.org/2000/svg","path");
    path.setAttribute("d","M2 1.5v22l6-6 4.6 10 3.2-1.5-4.6-9.8 8.5-.3z");
    svg.append(path);p.append(svg);
    r.append(s,rim,layer,p);
    h.__rim=rim;h.__layer=layer;h.__pointer=p;h.__state={};
    Object.defineProperty(window,"__phoenixDesktopAgentPointer",{value:h,writable:true,configurable:true});
    (document.body||document.documentElement).append(h);
  }
  const P=h.__pointer,L=h.__layer,R=h.__rim,S=h.__state;
  const x=a.x,y=a.y,act=a.action,dir=a.label||"";
  clearTimeout(h.__t1);clearTimeout(h.__t2);
  const mk=(cls,css)=>{const e=document.createElement("div");e.className="fx "+cls;if(css)e.style.cssText=css;return e};
  const add=(e,ms)=>{L.append(e);setTimeout(()=>e.remove(),ms)};
  L.querySelectorAll(".target,.chip").forEach(e=>e.remove());
  const visible=P.style.opacity==="1";
  // A comet trail from where the cursor was to where it is going.
  if(visible&&!reduce&&S.x!==undefined){
    const dx=x-S.x,dy=y-S.y,d=Math.hypot(dx,dy);
    if(d>12)add(mk("trail","left:"+S.x+"px;top:"+S.y+"px;width:"+d+"px;transform:rotate("+Math.atan2(dy,dx)+"rad)"),620);
  }
  S.x=x;S.y=y;
  if(!visible){P.style.transition="none";P.style.transform="translate3d("+x+"px,"+y+"px,0)";void P.offsetWidth;P.style.transition="";}
  else P.style.transform="translate3d("+x+"px,"+y+"px,0)";
  P.style.opacity="1";
  R.classList.add("on");
  const names={click:"Click",type:"Typing",select:"Choose option",press:dir||"Key",scroll:"Scroll "+({up:"↑",down:"↓",left:"←",right:"→"}[dir]||""),drag:"Drag",copy:"Copy",paste:"Paste"};
  const dur=act==="type"?1900:1150;
  let box=null;
  if(act!=="scroll"){
    let e=document.elementFromPoint(x,y);
    if(e){
      e=e.closest("a,button,input,select,textarea,summary,label,[role=button],[role=link],[role=tab],[role=menuitem],[role=checkbox],[role=radio],[role=option],[contenteditable=true]")||e;
      const q=e.getBoundingClientRect();
      if(q.width>=4&&q.height>=4&&q.width*q.height<0.45*innerWidth*innerHeight){
        box=q;
        const rad=Math.min(24,(parseFloat(getComputedStyle(e).borderTopLeftRadius)||6)+3);
        add(mk("target","left:"+(q.left-3)+"px;top:"+(q.top-3)+"px;width:"+(q.width+6)+"px;height:"+(q.height+6)+"px;border-radius:"+rad+"px;--d:"+dur+"ms"),dur+100);
      }
    }
    if(act!=="type"||!box){
      add(mk("ripple","left:"+x+"px;top:"+y+"px"),800);
      add(mk("ripple b","left:"+x+"px;top:"+y+"px"),900);
    }
  }else if(!reduce){
    const w={down:"left:0;right:0;bottom:0;top:auto;height:30%;--g:to top",up:"left:0;right:0;top:0;height:30%;--g:to bottom",left:"left:0;top:0;bottom:0;width:22%;--g:to right",right:"right:0;left:auto;top:0;bottom:0;width:22%;--g:to left"}[dir];
    if(w)add(mk("wash",w),760);
  }
  const text=names[act];
  if(text){
    const chip=mk("chip","--d:"+dur+"ms");
    chip.textContent=text;
    const cx=box?Math.max(8,Math.min(innerWidth-120,box.left)):Math.max(8,Math.min(innerWidth-120,x+18));
    const cy=box?(box.top>34?box.top-30:box.bottom+8):Math.max(8,Math.min(innerHeight-30,y+24));
    chip.style.left=cx+"px";chip.style.top=cy+"px";
    add(chip,dur+100);
  }
  h.__t1=setTimeout(()=>{P.style.opacity="0"},1600);
  h.__t2=setTimeout(()=>{R.classList.remove("on")},1900);
  return true;
})()`
