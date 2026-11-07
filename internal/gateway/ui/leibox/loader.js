import { mount } from './ai-generating/AiImageGeneration.ts';

// A decorative waiting indicator. Task status stays in the surrounding real UI.
class GateHomeLoader extends HTMLElement {
  connectedCallback() {
    this.setAttribute('aria-hidden','true');
    this.media=matchMedia('(prefers-reduced-motion: reduce)');
    this.controller=mount(this,{height:420,reducedMotion:this.media.matches});
    this.visible=true;
    this.preference=()=>this.controller?.update({reducedMotion:this.media.matches||!this.visible});
    this.media.addEventListener('change',this.preference);
    this.observer=new IntersectionObserver(([entry])=>{this.visible=entry.isIntersecting;this.preference();});
    this.observer.observe(this);
  }
  disconnectedCallback() {
    this.observer?.disconnect();
    this.media?.removeEventListener('change',this.preference);
    this.controller?.destroy();
    this.controller=null;
  }
}
customElements.define('gatehome-loader',GateHomeLoader);
