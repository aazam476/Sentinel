// SPDX-License-Identifier: Apache-2.0

// Package stacks runs Docker Compose stacks (§9.4): Sentinel's own, one
// folder each with compose.yaml and .env, and projects started some other
// way, found with `docker compose ls`. Compose has no stable Go API, so
// this uses the docker compose command, through command.Runner.
package stacks
