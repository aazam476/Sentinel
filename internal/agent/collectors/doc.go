// SPDX-License-Identifier: Apache-2.0

// Package collectors reads the machine's vital signs and details for the
// server: a metrics sample every 10 seconds (Sample) and a description of
// the machine when the server asks (SystemInfo).
//
// The readings come from a Source. The real one (NewHostSource) uses
// gopsutil, which works the same way on Linux, macOS and Windows; tests use
// a fake. A reading that fails is left out of the sample rather than
// failing the whole sample: a machine without temperature sensors still
// reports its CPU.
package collectors
