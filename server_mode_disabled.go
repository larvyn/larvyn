//go:build server

package main

// Larvyn's services run commands and read, write and delete files with the
// user's rights. Wails' server mode would serve them over HTTP without any
// authentication (issue #50), so building with -tags server is refused.
var _ = serverModeIsDisabled_SeeIssue50
