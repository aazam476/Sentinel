# 0025 — Alert rules and notifications

**Date:** 2026-11-09 · **Status:** Accepted

## Context

M5 raised storage alerts with fixed thresholds and told nobody. M6 adds alert rules (§9.9):
metric thresholds held for a while, machines offline, storage health and failed scheduled
scripts, on all machines, a tag or one machine; four notification channels (email over SMTP, a
generic JSON webhook, ntfy and Discord); acknowledging; a "resolved" notification; and a
maintenance mode that silences a machine. The "Done when" is a CPU threshold alert firing and
resolving with notifications on each channel.

## Decision

- **Every alert belongs to the rule that raised it.** A metric, offline or failed-script rule's
  alerts have the kind `rule.<id>`, so two rules on the same metric are separate alerts; storage
  alerts keep their M5 kinds (pool health, errors, capacity, SMART), and the first storage rule
  (by name) that raises one owns it. The migration adds a "Storage health" rule at 80 %, on all
  machines, so M5's behavior carries on and its threshold and channels become editable.
- **Metric and offline rules are evaluated by the server every minute**, from the readings it
  already stores. A metric rule fires when every reading in its window is past the threshold and
  the readings cover the window (allowing for one missed sample at its start); with no readings
  (the machine is off) its alert is left as it is, since the offline rule covers that. Storage
  rules are evaluated when the agent reports storage health, and failed-script rules when a
  scheduled run ends: the alert fires on a failure and resolves when that schedule next succeeds
  on that machine. Runs a person starts don't alert: they are watching.
- **An alert is deduplicated by machine, kind and subject**: while its condition holds it stays
  one alert, its message updated; once it resolves, a new one starts if the condition returns.
  Turning a rule off, retargeting it away from a machine or deleting it resolves its alerts.
- **Acknowledging** records who and when, and is audited; the alert keeps firing until its
  condition clears. Admins acknowledge, and manage rules and channels (`manage_alerts`).
  Everyone who can see the fleet sees alerts.
- **Notifications go out when an alert starts and when it resolves**, to the enabled channels of
  its rule, in the background with a 15-second limit each, so a slow channel holds up nothing.
  Each channel records when it last sent and why the last send failed, and the UI shows it; a
  "Send test" button sends a test notification and reports a failure as such (HTTP 502, not a
  server error). Email is plain text over SMTP with STARTTLS (the default), TLS or, for a relay
  on the same network, none; STARTTLS that the server doesn't offer is a failure, never a quiet
  fall back to plain text. Webhooks get the alert as JSON; Discord a one-line message; ntfy the
  message with a title, a priority that follows the severity and an emoji tag.
- **Channel settings are sealed with the secret key** (SEC-42), bound to the channel's type, like
  registry credentials. The SMTP password, the ntfy token and webhook URLs (which carry their own
  tokens) are never sent back to the browser; a change that leaves them empty keeps the stored
  ones.
- **Maintenance mode** is a time on the machine (`maintenance_until`), up to 30 days ahead, set
  and ended by admins and audited. During it alerts are raised and shown as usual, but nobody is
  notified, including when they resolve; the fleet and machine view say the machine is in
  maintenance.
- **Browsers hear about alerts as they change**: the live event stream's `alerts` event, already
  sent for storage alerts, is now also sent for the alerts the server raises itself.

## Consequences

- An alert that started during maintenance and resolves after it sends a "resolved"
  notification with no "firing" before it.
- Webhook and ntfy addresses can point anywhere the server can reach, including the local
  network; only admins can set them.
- Container and updates rules (§9.9) need agent events that don't exist yet; they are left for
  later.

## The Alerts page

- A top-level page for everyone: firing alerts first (critical, then newest), then the 200 most
  recently resolved, each with its machine, rule and times, and an "Acknowledge" button for
  admins.
- Admins also get Rules (a dialog that describes the condition in words as it is set, e.g. "CPU
  above 90% for 5 minutes") and Channels (type-specific fields, "Send test", the last error).
- The machine overview lists its firing alerts and, for admins, a Maintenance panel (1 hour, 4
  hours, 1 day or 1 week, or end it).
