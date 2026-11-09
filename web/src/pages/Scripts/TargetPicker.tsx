// SPDX-License-Identifier: Apache-2.0
import { useAgents } from '../../api/agents';
import type { ScriptTarget } from '../../api/types.gen';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from './Scripts.module.css';

/** TargetPicker chooses where a script runs: one machine, a tag, or several. */
export function TargetPicker({
  target,
  onChange,
}: {
  target: ScriptTarget;
  onChange: (target: ScriptTarget) => void;
}) {
  const agents = useAgents();
  const machines = agents.data?.agents ?? [];
  const tags = [...new Set(machines.flatMap((agent) => agent.tags))].sort((a, b) =>
    a.localeCompare(b),
  );
  const chosen = new Set(target.value.split(',').filter((id) => id !== ''));
  const kinds = [
    ['agent', 'One machine'],
    ['tag', 'Machines with a tag'],
    ['agents', 'Several machines'],
  ] as const;
  return (
    <fieldset className={styles.target}>
      <legend>Run on</legend>
      <div className={styles.kinds}>
        {kinds.map(([kind, label]) => (
          <label key={kind} className={styles.choice}>
            <input
              type="radio"
              name="target-kind"
              checked={target.kind === kind}
              onChange={() => {
                onChange({ kind, value: '' });
              }}
            />
            {label}
          </label>
        ))}
      </div>
      {target.kind === 'agent' && (
        <select
          className={systemStyles.select}
          aria-label="Machine"
          value={target.value}
          onChange={(event) => {
            onChange({ kind: 'agent', value: event.target.value });
          }}
        >
          <option value="">Choose a machine</option>
          {machines.map((agent) => (
            <option key={agent.id} value={agent.id}>
              {agent.name}
              {agent.online ? '' : ' (offline)'}
            </option>
          ))}
        </select>
      )}
      {target.kind === 'tag' && (
        <select
          className={systemStyles.select}
          aria-label="Tag"
          value={target.value}
          onChange={(event) => {
            onChange({ kind: 'tag', value: event.target.value });
          }}
        >
          <option value="">Choose a tag</option>
          {tags.map((tag) => (
            <option key={tag} value={tag}>
              {tag}
            </option>
          ))}
        </select>
      )}
      {target.kind === 'agents' && (
        <ul className={styles.machineChoices} aria-label="Machines">
          {machines.map((agent) => (
            <li key={agent.id}>
              <label className={styles.choice}>
                <input
                  type="checkbox"
                  checked={chosen.has(agent.id)}
                  onChange={(event) => {
                    const next = new Set(chosen);
                    if (event.target.checked) {
                      next.add(agent.id);
                    } else {
                      next.delete(agent.id);
                    }
                    onChange({ kind: 'agents', value: [...next].join(',') });
                  }}
                />
                <span className={styles.machineName}>{agent.name}</span>
                {!agent.online && <span className={styles.muted}>offline</span>}
              </label>
            </li>
          ))}
        </ul>
      )}
    </fieldset>
  );
}
