// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package services

// newSCM is only real on Windows.
func newSCM(Clock) Manager { return unsupported{} }
