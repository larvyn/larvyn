package main

import "time"

// shellEnvReady is closed once the login shell's PATH has been imported. It
// starts closed, so code that never calls startShellEnvImport (tests) does
// not wait.
var shellEnvReady = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

// startShellEnvImport imports the shell PATH in the background, so the window
// opens right away; lookups and launches wait for it through waitShellEnv.
// Call it from main before anything else runs.
func startShellEnvImport() {
	shellEnvReady = make(chan struct{})
	go func() {
		defer close(shellEnvReady)
		importShellPath()
	}()
}

// waitShellEnv blocks until the shell PATH is in place. It waits a little
// longer than the import itself may take, so a slow shell never makes a
// lookup like which("laravel") run against the bare desktop PATH.
func waitShellEnv() {
	select {
	case <-shellEnvReady:
	case <-time.After(12 * time.Second):
	}
}
