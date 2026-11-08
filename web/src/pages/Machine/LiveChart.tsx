// SPDX-License-Identifier: Apache-2.0
import { Liveline, type LivelinePoint } from 'liveline';
import { useState } from 'react';

import { useAgentMetrics } from '../../api/agents';
import type { MetricName } from '../../api/metricNames';
import type { CurrentMetrics } from '../../api/types.gen';
import { useMediaQuery } from '../../hooks/useMediaQuery';
import styles from './MachinePage.module.css';

interface LiveChartProps {
  agentId: string;
  metric: MetricName;
  title: string;
  current: CurrentMetrics | null;
  formatValue: (value: number) => string;
}

/** windowSeconds is how much recent history a live chart shows. */
const windowSeconds = 10 * 60;

/**
 * LiveChart scrolls a metric as samples arrive. It starts from the last
 * ten minutes on the server, then adds each sample the event stream
 * delivers. Liveline draws on a canvas and interpolates between samples
 * itself, so React only renders when a sample arrives (every 10 seconds).
 *
 * Motion is kept to what the data needs: the line glides between samples
 * (snapping instead under reduced motion), and nothing loops or pulses on
 * a screen people keep open.
 */
export function LiveChart({ agentId, metric, title, current, formatValue }: LiveChartProps) {
  const [openedAt] = useState(() => Date.now());
  const history = useAgentMetrics(agentId, metric, openedAt - windowSeconds * 1000, openedAt);
  const [arrived, setArrived] = useState<LivelinePoint[]>([]);
  const [lastSampleTime, setLastSampleTime] = useState<number | null>(null);
  const isDark = useMediaQuery('(prefers-color-scheme: dark)');
  const reduceMotion = useMediaQuery('(prefers-reduced-motion: reduce)');

  // Adds each new sample once, while rendering (React's pattern for state
  // derived from changing props) rather than in an effect.
  const value = current?.values[metric];
  if (current !== null && current.time !== lastSampleTime) {
    setLastSampleTime(current.time);
    if (value !== undefined) {
      setArrived((points) => [
        ...points.slice(-windowSeconds),
        { time: current.time / 1000, value },
      ]);
    }
  }

  const seeded = (history.data?.points ?? []).map((point) => ({
    time: point.time / 1000,
    value: point.value,
  }));
  const lastSeeded = seeded.at(-1)?.time ?? 0;
  const data = [...seeded, ...arrived.filter((point) => point.time > lastSeeded)];

  return (
    <figure className={styles.liveChart}>
      <figcaption className={styles.chartTitle}>
        {title}
        <span className={styles.chartValue}>{value === undefined ? '–' : formatValue(value)}</span>
      </figcaption>
      <div
        className={styles.liveCanvas}
        role="img"
        aria-label={`${title} over the last 10 minutes`}
      >
        <Liveline
          data={data}
          value={value ?? data.at(-1)?.value ?? 0}
          loading={history.isPending}
          theme={isDark ? 'dark' : 'light'}
          color={isDark ? '#6b8ff5' : '#2453d6'}
          window={windowSeconds}
          formatValue={formatValue}
          badge={false}
          momentum={false}
          pulse={false}
          lerpSpeed={reduceMotion ? 1 : 0.08}
          emptyText="No readings yet"
        />
      </div>
    </figure>
  );
}
