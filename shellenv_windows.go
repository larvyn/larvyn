package main

// importShellPath is a no-op on Windows: GUI apps get the full user PATH from
// the registry, there is no login shell to ask.
func importShellPath() {}
