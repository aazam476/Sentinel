// SPDX-License-Identifier: Apache-2.0

// Package clitest builds Sentinel's binaries and runs them the way a user
// would, for the command-line contract tests in cmd/.
//
// Those tests are black-box: they only see exit codes, output and network
// behavior. They carry the "integration" build tag, so `make test` skips them
// and `make test-integration` runs them.
package clitest
