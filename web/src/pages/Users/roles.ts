// SPDX-License-Identifier: Apache-2.0

/** roles are what each role may do (§8.6), for choosing one. */
export const roles = [
  { value: 'viewer', label: 'Viewer', description: 'Sees dashboards, metrics, storage and logs.' },
  {
    value: 'operator',
    label: 'Operator',
    description: 'Also uses terminals, files, services, Docker and scripts.',
  },
  {
    value: 'admin',
    label: 'Admin',
    description: 'Everything, including users, machines, alerts and the audit log.',
  },
] as const;

export function roleLabel(role: string): string {
  return roles.find((item) => item.value === role)?.label ?? role;
}
