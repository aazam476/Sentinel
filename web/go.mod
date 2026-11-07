// This file is not a real Go module. Its presence makes the Go tools treat
// web/ as a separate module, so "go test ./..." and the linters skip the Go
// files that some npm packages ship inside web/node_modules.
module github.com/aazam476/sentinel/web

go 1.27
