// SPDX-License-Identifier: Apache-2.0
import { type MetricName, metricNames } from '../../api/metricNames';
import { formatPercent, formatRate } from '../../formatting/formatting';

/** ChartMetric is a metric the machine page can chart, with its format. */
export interface ChartMetric {
  name: MetricName;
  label: string;
  format: (value: number) => string;
}

const formatCelsius = (value: number) => `${value.toFixed(0)} °C`;

/** defaultMetric is the metric a chart shows first. */
export const defaultMetric: ChartMetric = {
  name: metricNames.cpu,
  label: 'CPU',
  format: (value) => formatPercent(value),
};

/** chartMetrics lists the metrics in the order the UI offers them. */
export const chartMetrics: ChartMetric[] = [
  defaultMetric,
  { name: metricNames.memory, label: 'Memory', format: (value) => formatPercent(value) },
  { name: metricNames.disk, label: 'Fullest disk', format: (value) => formatPercent(value) },
  { name: metricNames.networkReceived, label: 'Network in', format: (value) => formatRate(value) },
  { name: metricNames.networkSent, label: 'Network out', format: (value) => formatRate(value) },
  { name: metricNames.diskRead, label: 'Disk reads', format: (value) => formatRate(value) },
  { name: metricNames.diskWritten, label: 'Disk writes', format: (value) => formatRate(value) },
  { name: metricNames.temperature, label: 'Temperature', format: formatCelsius },
];

/** HistoryRange is a span of history the charts can show. */
export interface HistoryRange {
  label: string;
  milliseconds: number;
}

const hour = 3_600_000;

/** defaultRange is the span the history chart shows first. */
export const defaultRange: HistoryRange = { label: '24 h', milliseconds: 24 * hour };

export const historyRanges: HistoryRange[] = [
  { label: '1 h', milliseconds: hour },
  defaultRange,
  { label: '7 d', milliseconds: 7 * 24 * hour },
  { label: '30 d', milliseconds: 30 * 24 * hour },
  { label: '1 y', milliseconds: 365 * 24 * hour },
];

/**
 * formatAxisTime labels the time axis: clock times for spans of a day or
 * less, dates beyond that.
 */
export function formatAxisTime(milliseconds: number, span: number, locale?: string): string {
  const options: Intl.DateTimeFormatOptions =
    span <= 24 * hour ? { hour: '2-digit', minute: '2-digit' } : { month: 'short', day: 'numeric' };
  return new Intl.DateTimeFormat(locale, options).format(milliseconds);
}
