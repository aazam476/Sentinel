// SPDX-License-Identifier: Apache-2.0
import type { Agent } from '../../api/types.gen';

/** filterAgents keeps the machines whose name or hostname contains search and that have tag. */
export function filterAgents(agents: Agent[], search: string, tag: string | null): Agent[] {
  const needle = search.trim().toLocaleLowerCase();
  return agents.filter(
    (agent) =>
      (tag === null || agent.tags.includes(tag)) &&
      (needle === '' ||
        agent.name.toLocaleLowerCase().includes(needle) ||
        agent.hostname.toLocaleLowerCase().includes(needle)),
  );
}
