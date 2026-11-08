// SPDX-License-Identifier: Apache-2.0

// Package alerts keeps each machine's alerts (§9.9): conditions that need
// a person's attention, firing while they hold and resolved once they
// clear. So far the conditions come from storage health (§9.5): a pool
// that isn't ONLINE, vdev errors, a scrub that found errors, a pool or
// quota nearly full, and SMART failures. Rules and notifications build on
// this in M6.
package alerts
