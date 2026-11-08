// SPDX-License-Identifier: Apache-2.0

// Package fakerunner is a command.Runner for tests. It answers only the
// exact commands a test expects, so a test fails if the code under test
// builds a command line differently, which is how argument injection bugs
// would show.
package fakerunner
