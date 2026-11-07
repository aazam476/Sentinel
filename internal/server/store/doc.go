// SPDX-License-Identifier: Apache-2.0

// Package store is the server's database: a single SQLite file,
// <data dir>/sentinel.db, accessed with hand-written SQL.
//
// cmd/sentinel-server calls Open once at startup. Open applies any pending
// migrations from the migrations/ folder and starts the writer goroutine.
// Other packages (auth, audit, ...) receive the *Store and run their queries
// through two methods:
//
//   - Read runs a function inside a read-only transaction. Many reads can
//     run at the same time.
//   - Write runs a function inside a read-write transaction. All writes go
//     through one goroutine, one at a time, because SQLite only allows a
//     single writer; queuing them in Go is clearer and faster than letting
//     them fight over the database lock and retry.
//
// The SQL for each feature lives in the package that owns the feature, next
// to the code that uses it. This package only provides the connection,
// transactions and the schema.
//
// Migrations are numbered .sql files (0001_init.sql, 0002_...). They only
// ever move forward: a released migration is never edited, a later one
// changes what an earlier one did.
package store
