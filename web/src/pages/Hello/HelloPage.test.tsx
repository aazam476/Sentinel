// SPDX-License-Identifier: Apache-2.0
import { render, screen } from '@testing-library/react';

import { App } from '../../App';
import { findAccessibilityViolations } from '../../test/axe';
import { HelloPage } from './HelloPage';

describe('HelloPage', () => {
  it('greets the visitor with a top-level heading', () => {
    render(<HelloPage />);

    expect(screen.getByRole('heading', { level: 1, name: 'Hello Sentinel' })).toBeVisible();
  });

  it('tells the visitor the server is running', () => {
    render(<HelloPage />);

    expect(screen.getByText('The server is running.')).toBeVisible();
  });

  it('puts its content in the main landmark', () => {
    render(<HelloPage />);

    expect(screen.getByRole('main')).toContainElement(screen.getByRole('heading', { level: 1 }));
  });

  it('has no accessibility violations', async () => {
    const { container } = render(<HelloPage />);

    expect(await findAccessibilityViolations(container)).toEqual([]);
  });
});

describe('App', () => {
  it('shows the hello page', () => {
    render(<App />);

    expect(screen.getByRole('heading', { level: 1, name: 'Hello Sentinel' })).toBeVisible();
  });
});
