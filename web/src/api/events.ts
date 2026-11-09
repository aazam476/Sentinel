// SPDX-License-Identifier: Apache-2.0
import { type QueryClient, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';

import { agentKeys } from './agents';
import { apiURL } from './client';
import type { AgentDetails, AgentEvent, AgentsResponse, MetricsEvent } from './types.gen';

/**
 * useFleetEvents keeps machine data live while the user is signed in. It
 * listens to the server's event stream (/api/events): a machine coming
 * online, going offline or reporting new details refetches it (and an
 * alert starting or resolving, the alert lists too); a new
 * metrics sample is written straight into the cached machine and list, so
 * the numbers move without a request.
 *
 * The browser's EventSource reconnects by itself after a dropped
 * connection or a server restart.
 */
export function useFleetEvents() {
  const queryClient = useQueryClient();
  // An effect because the stream is a subscription to something outside
  // React that must be closed when the signed-in shell unmounts.
  useEffect(() => {
    if (typeof EventSource === 'undefined') {
      return undefined;
    }
    const source = new EventSource(apiURL('api/events'), { withCredentials: true });
    source.addEventListener('agent', (event) => {
      handleAgentEvent(queryClient, parse(event) as AgentEvent | undefined);
    });
    source.addEventListener('metrics', (event) => {
      handleMetricsEvent(queryClient, parse(event) as MetricsEvent | undefined);
    });
    return () => {
      source.close();
    };
  }, [queryClient]);
}

/** parse reads an event's JSON data; the server's types say what it holds. */
function parse(event: Event): unknown {
  try {
    return JSON.parse((event as MessageEvent<string>).data) as unknown;
  } catch {
    return undefined;
  }
}

export function handleAgentEvent(queryClient: QueryClient, event: AgentEvent | undefined) {
  if (event === undefined) {
    return;
  }
  void queryClient.invalidateQueries({ queryKey: agentKeys.list() });
  void queryClient.invalidateQueries({ queryKey: agentKeys.detail(event.agentId) });
  if (event.kind === 'alerts') {
    void queryClient.invalidateQueries({ queryKey: [...agentKeys.all, 'alerts'] });
  }
}

export function handleMetricsEvent(queryClient: QueryClient, event: MetricsEvent | undefined) {
  if (event === undefined) {
    return;
  }
  queryClient.setQueryData<AgentDetails>(agentKeys.detail(event.agentId), (agent) =>
    agent === undefined ? agent : { ...agent, online: true, current: event.current },
  );
  queryClient.setQueryData<AgentsResponse>(agentKeys.list(), (list) =>
    list === undefined
      ? list
      : {
          agents: list.agents.map((agent) =>
            agent.id === event.agentId ? { ...agent, online: true, current: event.current } : agent,
          ),
        },
  );
}
