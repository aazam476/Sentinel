// SPDX-License-Identifier: Apache-2.0
import type { AlertRule, AlertRuleTarget } from '../../api/types.gen';

/** ruleTypes are the kinds of rule, as the rule dialog offers them. */
export const ruleTypes = [
  { value: 'metric', label: 'Metric threshold' },
  { value: 'offline', label: 'Machine offline' },
  { value: 'storage', label: 'Storage health' },
  { value: 'script_failed', label: 'Failed scheduled script' },
] as const;

/** watchableMetrics are the metrics a metric rule can watch. */
export const watchableMetrics = [
  { value: 'cpu.percent', label: 'CPU', unit: '%' },
  { value: 'memory.percent', label: 'Memory', unit: '%' },
  { value: 'disk.percent', label: 'Disk (fullest)', unit: '%' },
  { value: 'temperature', label: 'Temperature', unit: ' °C' },
] as const;

/** channelTypes are the ways of sending notifications. */
export const channelTypes = [
  { value: 'email', label: 'Email' },
  { value: 'webhook', label: 'Webhook' },
  { value: 'ntfy', label: 'ntfy' },
  { value: 'discord', label: 'Discord' },
] as const;

export function labelOf(list: readonly { value: string; label: string }[], value: string): string {
  return list.find((item) => item.value === value)?.label ?? value;
}

function minutes(count: number): string {
  return count === 1 ? '1 minute' : `${String(count)} minutes`;
}

/** describeCondition says in words when a rule fires. */
export function describeCondition(rule: Pick<AlertRule, 'type' | 'config'>): string {
  const { config } = rule;
  switch (rule.type) {
    case 'metric': {
      const metric = watchableMetrics.find((item) => item.value === config.metric);
      const name = metric?.label ?? config.metric ?? '';
      return `${name} ${config.direction ?? 'above'} ${String(config.threshold ?? 0)}${metric?.unit ?? ''} for ${minutes(config.minutes ?? 0)}`;
    }
    case 'offline':
      return `Offline for more than ${minutes(config.minutes ?? 0)}`;
    case 'storage':
      return `Pool or disk problems, or more than ${String(config.capacityPercent ?? 80)}% full`;
    case 'script_failed':
      return 'A scheduled script fails';
  }
  return rule.type;
}

/** describeTarget says which machines a rule watches. */
export function describeTarget(target: AlertRuleTarget, names: Map<string, string>): string {
  switch (target.kind) {
    case 'tag':
      return `Machines tagged ${target.value}`;
    case 'agent':
      return names.get(target.value) ?? 'One machine';
  }
  return 'All machines';
}
