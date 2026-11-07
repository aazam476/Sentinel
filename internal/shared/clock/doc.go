// SPDX-License-Identifier: Apache-2.0

// Package clock provides the real clock that production code passes to
// anything that needs the current time.
//
// Code never calls time.Now directly. Instead, each package that needs the
// time declares a one-method interface:
//
//	type Clock interface { Now() time.Time }
//
// and receives clock.System{} in production, or a fake clock from
// internal/testutil/fakeclock in tests. Tests can then move time forward
// instantly instead of sleeping.
package clock
