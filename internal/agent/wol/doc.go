// SPDX-License-Identifier: Apache-2.0

// Package wol wakes a machine with a Wake-on-LAN magic packet (§9.6): six
// 0xFF bytes and then the target's MAC address sixteen times, sent by UDP
// to port 9 of the broadcast address of a network this machine is on. The
// agent only sends, never listens (SEC-26), and only to its own networks,
// so a server can't use it to send packets anywhere else.
package wol
