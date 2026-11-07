// SPDX-License-Identifier: Apache-2.0

package clock

import "time"

// System is the real wall clock.
type System struct{}

// Now returns the current time.
func (System) Now() time.Time {
	return time.Now()
}
