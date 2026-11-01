package main

import (
	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/updater"
)

// The types below moved to internal packages. The aliases keep their names in
// main, so the services' signatures and the frontend imports stay the same.

// Project is a folder Larvyn manages, usually a Laravel application.
type Project = store.Project

// Settings are the user's global preferences.
type Settings = store.Settings

// RunResult is the captured outcome of a short, non-interactive command.
type RunResult = proc.RunResult

// UpdateInfo tells the window whether a newer release exists.
type UpdateInfo = updater.Info
