// SPDX-License-Identifier: Apache-2.0

// Command echoagent stands in for sentinel-agent in the install scripts'
// tests: it prints the arguments it was run with, so a test can see the
// script ran the downloaded build with the right flags.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println("echoagent ran:", strings.Join(os.Args[1:], " "))
}
