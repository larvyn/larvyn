package system

import (
	"os"
	"path/filepath"

	"larvyn/internal/composer"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// HasComposer reports whether Composer can run, from PATH or Larvyn's phar.
func (s *SystemService) HasComposer() bool { return composer.Argv() != nil }

// InstallComposer downloads the official installer, verifies its signature
// and installs composer.phar into Larvyn's folder.
func (s *SystemService) InstallComposer() (terminal.SessionInfo, error) {
	dir := filepath.Dir(composer.Phar())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return terminal.SessionInfo{}, err
	}
	script := filepath.Join(dir, "install-composer.php")
	if err := os.WriteFile(script, []byte(composerSetup), 0o644); err != nil {
		return terminal.SessionInfo{}, err
	}
	return terminal.Sessions.Start(terminal.SessionSpec{Kind: "run", Key: "composer-setup", Title: "Install Composer", Cwd: dir,
		Argv: []string{store.PHP(), script}})
}

// UpdateLaravelInstaller updates the global laravel/installer in a terminal.
func (s *SystemService) UpdateLaravelInstaller() (terminal.SessionInfo, error) {
	home, _ := os.UserHomeDir()
	return terminal.Sessions.Start(terminal.SessionSpec{Kind: "run", Key: "installer-update", Title: "Update the Laravel installer", Cwd: home,
		Argv: []string{"composer", "global", "update", "laravel/installer", "--with-all-dependencies"}})
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
