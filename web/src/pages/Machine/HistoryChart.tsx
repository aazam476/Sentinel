// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { useState } from 'react';
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { useAgentMetrics } from '../../api/agents';
import { FormError } from '../../components/FormError/FormError';
import { formatDateTime } from '../../formatting/formatting';
import {
  chartMetrics,
  defaultMetric,
  defaultRange,
  formatAxisTime,
  historyRanges,
} from './chartOptions';
import styles from './MachinePage.module.css';

interface HistoryChartProps {
  agentId: string;
}

/**
 * HistoryChart shows one metric over an hour to a year. Longer ranges
 * come from hourly averages; the shaded band is each bucket's minimum and
 * maximum, so a short spike stays visible. Nothing animates: this is data
 * people read.
 */
export function HistoryChart({ agentId }: HistoryChartProps) {
  const [metric, setMetric] = useState(defaultMetric);
  const [range, setRange] = useState(defaultRange);
  // The range ends at the moment it was picked, so the query key (and the
  // cached series) stays the same until the user picks again.
  const [now, setNow] = useState(() => Date.now());
  const series = useAgentMetrics(agentId, metric.name, now - range.milliseconds, now);
  const points = (series.data?.points ?? []).map((point) => ({
    ...point,
    band: [point.min, point.max],
  }));

  return (
    <section className={styles.panel} aria-labelledby="history-title">
      <div className={styles.panelHeader}>
        <h2 id="history-title" className={styles.panelTitle}>
          History
        </h2>
        <select
          className={styles.select}
          aria-label="Metric"
          value={metric.name}
          onChange={(event) => {
            setMetric(
              chartMetrics.find((option) => option.name === event.target.value) ?? defaultMetric,
            );
          }}
        >
          {chartMetrics.map((option) => (
            <option key={option.name} value={option.name}>
              {option.label}
            </option>
          ))}
        </select>
      </div>
      <div className={styles.segmented} role="group" aria-label="Time range">
        {historyRanges.map((option) => (
          <button
            key={option.label}
            type="button"
            aria-pressed={option === range}
            className={clsx(styles.segment, option === range && styles.segmentActive)}
            onClick={() => {
              setRange(option);
              setNow(Date.now());
            }}
          >
            {option.label}
          </button>
        ))}
      </div>
      <div
        className={styles.historyCanvas}
        role="img"
        aria-label={`${metric.label} over ${range.label}`}
      >
        {points.length === 0 ? (
          <p className={styles.chartEmpty}>
            {series.isPending ? 'Loading…' : 'No readings in this range.'}
          </p>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <ComposedChart data={points} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} className={styles.grid} />
              <XAxis
                dataKey="time"
                type="number"
                domain={['dataMin', 'dataMax']}
                scale="time"
                tickFormatter={(time: number) => formatAxisTime(time, range.milliseconds)}
                className={styles.axis}
                minTickGap={32}
              />
              <YAxis tickFormatter={metric.format} width={64} className={styles.axis} />
              <Tooltip
                labelFormatter={(time) => formatDateTime(Number(time))}
                formatter={(value) =>
                  Array.isArray(value)
                    ? value.map((part) => metric.format(Number(part))).join(' – ')
                    : metric.format(Number(value))
                }
              />
              {series.data?.resolution !== 'raw' && (
                <Area
                  dataKey="band"
                  name="Range"
                  className={styles.band}
                  stroke="none"
                  isAnimationActive={false}
                />
              )}
              <Line
                dataKey="value"
                name="Average"
                className={styles.line}
                dot={false}
                strokeWidth={2}
                isAnimationActive={false}
              />
            </ComposedChart>
          </ResponsiveContainer>
        )}
      </div>
      <FormError error={series.error} />
    </section>
  );
}
