// SPDX-License-Identifier: Apache-2.0
import { metricNames } from '../../api/metricNames';
import { formatPercent } from '../../formatting/formatting';
import { Capabilities } from './Capabilities';
import { Filesystems } from './Filesystems';
import { HistoryChart } from './HistoryChart';
import { LiveChart } from './LiveChart';
import { useMachine } from './machineContext';
import { MachineSettings } from './MachineSettings';
import styles from './MachinePage.module.css';
import { SystemDetails } from './SystemDetails';

/** MachineOverview is a machine's charts, details and settings. */
export function MachineOverview() {
  const { session, machine } = useMachine();
  return (
    <>
      <div className={styles.liveGrid}>
        <LiveChart
          agentId={machine.id}
          metric={metricNames.cpu}
          title="CPU"
          current={machine.current}
          formatValue={formatPercent}
        />
        <LiveChart
          agentId={machine.id}
          metric={metricNames.memory}
          title="Memory"
          current={machine.current}
          formatValue={formatPercent}
        />
      </div>
      <HistoryChart agentId={machine.id} />
      <div className={styles.panelGrid}>
        <SystemDetails agent={machine} />
        <Capabilities capabilities={machine.capabilities} />
      </div>
      <Filesystems filesystems={machine.filesystems} isOnline={machine.online} />
      {session.user.role === 'admin' && <MachineSettings key={machine.id} agent={machine} />}
    </>
  );
}
