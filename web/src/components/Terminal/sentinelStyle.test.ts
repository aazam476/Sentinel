// SPDX-License-Identifier: Apache-2.0
import { afterEach, describe, expect, it } from 'vitest';

import { defineSentinelStyle } from './sentinelStyle';

describe('sentinel-style', () => {
  afterEach(() => {
    document.body.replaceChildren();
  });

  it('applies the CSS written to it through an adopted stylesheet, and shows no text', () => {
    defineSentinelStyle();
    defineSentinelStyle(); // twice is harmless
    const element = document.createElement('sentinel-style');
    document.body.append(element);
    element.textContent = '.xterm { color: red; }';

    expect(element.textContent).toBe('.xterm { color: red; }');
    expect(element.childNodes).toHaveLength(0);
    expect(element.hidden).toBe(true);
    expect(document.adoptedStyleSheets).toHaveLength(1);
  });

  it('removes its stylesheet when it leaves the page', () => {
    defineSentinelStyle();
    const element = document.createElement('sentinel-style');
    document.body.append(element);
    element.textContent = null;
    expect(element.textContent).toBe('');
    element.remove();
    expect(document.adoptedStyleSheets).toHaveLength(0);
  });
});
