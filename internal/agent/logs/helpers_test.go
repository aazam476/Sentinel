// SPDX-License-Identifier: Apache-2.0

package logs_test

import (
	"fmt"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

func typeName(value any) string { return fmt.Sprintf("%T", value) }
