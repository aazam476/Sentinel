// SPDX-License-Identifier: Apache-2.0

/**
 * The metrics the server stores for every machine, as named in
 * internal/server/metrics/names.go. CurrentMetrics.values and the metrics
 * endpoint use these names.
 */
export const metricNames = {
  cpu: 'cpu.percent',
  memory: 'memory.percent',
  disk: 'disk.percent',
  networkReceived: 'network.received',
  networkSent: 'network.sent',
  diskRead: 'disk.read',
  diskWritten: 'disk.written',
  temperature: 'temperature.max',
} as const;

export type MetricName = (typeof metricNames)[keyof typeof metricNames];
