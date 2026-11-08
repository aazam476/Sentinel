// SPDX-License-Identifier: Apache-2.0
import { vi } from 'vitest';

import type { AgentEvent, MetricsEvent } from '../api/types.gen';

/**
 * FakeEventSource stands in for the browser's EventSource, which jsdom
 * lacks, so tests can play the server's event stream.
 */
export class FakeEventSource extends EventTarget {
  static latest: FakeEventSource | undefined;
  readonly url: string;
  closed = false;

  constructor(url: string) {
    super();
    this.url = url;
    FakeEventSource.latest = this;
  }

  close() {
    this.closed = true;
  }

  /** emit sends an event the way the server would. */
  emit(name: 'agent' | 'metrics', data: AgentEvent | MetricsEvent) {
    this.dispatchEvent(new MessageEvent(name, { data: JSON.stringify(data) }));
  }
}

/** useFakeEventSource installs FakeEventSource for the current test. */
export function useFakeEventSource() {
  FakeEventSource.latest = undefined;
  vi.stubGlobal('EventSource', FakeEventSource);
}

/** eventStream returns the stream the app opened, failing if there is none. */
export function eventStream(): FakeEventSource {
  const source = FakeEventSource.latest;
  if (source === undefined) {
    throw new Error('the app opened no event stream');
  }
  return source;
}
