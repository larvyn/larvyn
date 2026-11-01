package main

import (
	"os"
	"path/filepath"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

// Larvyn can run Composer even when it is not installed: it keeps its own
// composer.phar and runs it with the configured PHP.

func composerPhar() string {
	return filepath.Join(store.HomeDir(), "bin", "composer.phar")
}

// composerArgv returns the command that runs Composer, or nil when there is
// none yet.
func composerArgv() []string {
	if p := proc.Which("composer"); p != "" {
		return []string{p}
	}
	if _, err := os.Stat(composerPhar()); err == nil {
		return []string{store.PHP(), composerPhar()}
	}
	return nil
}

// resolveProgram rewrites argv so a bare "composer" uses Larvyn's own phar
// when Composer is not on PATH.
func resolveProgram(argv []string) []string {
	if len(argv) > 0 && argv[0] == "composer" {
		if c := composerArgv(); c != nil {
			return append(c, argv[1:]...)
		}
	}
	return argv
}

// HasComposer reports whether Composer can run, from PATH or Larvyn's phar.
func (s *SystemService) HasComposer() bool { return composerArgv() != nil }

// InstallComposer downloads the official installer, verifies its signature
// and installs composer.phar into Larvyn's folder.
func (s *SystemService) InstallComposer() (SessionInfo, error) {
	dir := filepath.Dir(composerPhar())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SessionInfo{}, err
	}
	script := filepath.Join(dir, "install-composer.php")
	if err := os.WriteFile(script, []byte(composerSetup), 0o644); err != nil {
		return SessionInfo{}, err
	}
	return terminals.Start(SessionSpec{Kind: "run", Key: "composer-setup", Title: "Install Composer", Cwd: dir,
		Argv: []string{store.PHP(), script}})
}

const composerSetup = `<?php
echo 'Downloading the Composer installer...', PHP_EOL;
$sig = trim(file_get_contents('https://composer.github.io/installer.sig'));
copy('https://getcomposer.org/installer', 'composer-setup.php');
if (hash_file('sha384', 'composer-setup.php') !== $sig) {
    unlink('composer-setup.php');
    fwrite(STDERR, 'Installer signature does not match, aborting.' . PHP_EOL);
    exit(1);
}
echo 'Signature verified.', PHP_EOL;
passthru(escapeshellarg(PHP_BINARY) . ' composer-setup.php --filename=composer.phar', $code);
unlink('composer-setup.php');
exit($code);
`
