// SPDX-License-Identifier: Apache-2.0

/**
 * <sentinel-style> stands in for the <style> elements xterm.js creates (see
 * xtermWithoutInlineStyles in vite.config.ts). The Content-Security-Policy
 * blocks inline <style> elements, but not constructed stylesheets, so the
 * CSS written to this element's textContent is applied through one, adopted
 * by the document while the element is on the page. The element itself
 * holds no text and shows nothing.
 */
class SentinelStyle extends HTMLElement {
  private readonly sheet = new CSSStyleSheet();
  private css = '';

  connectedCallback() {
    this.hidden = true;
    if (!document.adoptedStyleSheets.includes(this.sheet)) {
      document.adoptedStyleSheets = [...document.adoptedStyleSheets, this.sheet];
    }
  }

  disconnectedCallback() {
    document.adoptedStyleSheets = document.adoptedStyleSheets.filter(
      (sheet) => sheet !== this.sheet,
    );
  }

  /** setCSS replaces the element's CSS; textContent writes land here. */
  setCSS(css: string) {
    this.css = css;
    this.sheet.replaceSync(css);
  }

  getCSS(): string {
    return this.css;
  }
}

// xterm.js assigns textContent, which Node defines as an accessor. A class
// can't redeclare it as one in TypeScript, so it is defined on the
// prototype here instead.
Object.defineProperty(SentinelStyle.prototype, 'textContent', {
  configurable: true,
  get(this: SentinelStyle) {
    return this.getCSS();
  },
  set(this: SentinelStyle, value: string | null) {
    this.setCSS(value ?? '');
  },
});

/** defineSentinelStyle registers <sentinel-style>, once. */
export function defineSentinelStyle() {
  if (customElements.get('sentinel-style') === undefined) {
    customElements.define('sentinel-style', SentinelStyle);
  }
}
