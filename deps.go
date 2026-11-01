package main

import (
	"encoding/json"
	"errors"
	"time"

	"larvyn/internal/proc"
)

// composerJSON runs a Composer command that prints JSON and returns it.
func composerJSON(projectID string, args ...string) (json.RawMessage, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return nil, err
	}
	c := composerArgv()
	if c == nil {
		return nil, errors.New("Composer is not installed: install it from Settings → Toolchain")
	}
	res, err := proc.Run(dir, 3*time.Minute, nil, c[0], append(append(c[1:], args...), "--no-interaction", "--no-ansi")...)
	if err != nil {
		return nil, err
	}
	out := extractJSON(res.Stdout)
	if out == nil {
		return nil, errors.New(proc.StripANSI(proc.FirstNonEmpty(res.Stderr, res.Stdout, "composer printed no JSON")))
	}
	return out, nil
}

// Outdated returns `composer outdated --direct` as JSON.
func (l *LaravelService) Outdated(projectID string) (json.RawMessage, error) {
	return composerJSON(projectID, "outdated", "--direct", "--format=json")
}

// Audit returns `composer audit` (security advisories) as JSON. Composer
// exits non-zero when it finds advisories, which is not a failure here.
func (l *LaravelService) Audit(projectID string) (json.RawMessage, error) {
	return composerJSON(projectID, "audit", "--format=json", "--locked")
}
