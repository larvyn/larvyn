<?php

// Larvyn's stand-in for `laravel new` when the Laravel installer is not
// installed. Mirrors what laravel/installer does for the options the New app
// dialog offers: starter kit, database, git, Pest and npm.
//
// Usage: php create.php <base64 JSON>
// {"parent","name","starter","testing","db","composer":[argv...]}

$opts = json_decode(base64_decode($argv[1] ?? ''), true);
if (! is_array($opts) || empty($opts['name']) || empty($opts['parent']) || empty($opts['composer'])) {
    fwrite(STDERR, "create.php: bad arguments\n");
    exit(2);
}

$name = $opts['name'];
$parent = $opts['parent'];
$dir = $parent.DIRECTORY_SEPARATOR.$name;
$composer = $opts['composer'];
$php = PHP_BINARY;
$db = $opts['db'] ?: 'sqlite';
$pest = ($opts['testing'] ?? 'pest') !== 'phpunit';
$kits = ['react' => 'laravel/react-starter-kit', 'vue' => 'laravel/vue-starter-kit', 'livewire' => 'laravel/livewire-starter-kit'];
$kit = $kits[$opts['starter'] ?? ''] ?? null;

function step(string $title): void
{
    fwrite(STDOUT, "\n\033[1;34m➜\033[0m \033[1m{$title}\033[0m\n");
}

function warn(string $message): void
{
    fwrite(STDOUT, "\033[33m! {$message}\033[0m\n");
}

/** Runs a program with the terminal attached and returns its exit code. */
function run(array $cmd, string $cwd, array $env = []): int
{
    // Batch files (composer.bat, npm.cmd) need cmd.exe on Windows.
    if (PHP_OS_FAMILY === 'Windows' && (preg_match('/\.(bat|cmd)$/i', $cmd[0]) || in_array($cmd[0], ['npm', 'npx'], true))) {
        array_unshift($cmd, 'cmd', '/c');
    }
    $fullEnv = $env ? array_merge(getenv(), $env) : null;
    $proc = @proc_open($cmd, [STDIN, STDOUT, STDERR], $pipes, $cwd, $fullEnv);
    if (! is_resource($proc)) {
        warn('could not start '.$cmd[0]);

        return 127;
    }

    return proc_close($proc);
}

function haveGit(): bool
{
    $null = PHP_OS_FAMILY === 'Windows' ? 'NUL' : '/dev/null';
    exec('git --version 2>'.$null, $out, $code);

    return $code === 0;
}

function replaceInFile(string $file, array $search, array $replace): void
{
    if (is_file($file)) {
        file_put_contents($file, str_replace($search, $replace, file_get_contents($file)));
    }
}

function configureDatabase(string $dir, string $db, string $name): void
{
    foreach (['.env', '.env.example'] as $f) {
        $file = $dir.DIRECTORY_SEPARATOR.$f;
        if (is_file($file)) {
            file_put_contents($file, preg_replace('/DB_CONNECTION=.*/', 'DB_CONNECTION='.$db, file_get_contents($file)));
        }
    }
    $defaults = ['DB_HOST=127.0.0.1', 'DB_PORT=3306', 'DB_DATABASE=laravel', 'DB_USERNAME=root', 'DB_PASSWORD='];
    $commented = array_map(fn ($d) => "# {$d}", $defaults);
    foreach (['.env', '.env.example'] as $f) {
        $file = $dir.DIRECTORY_SEPARATOR.$f;
        if ($db === 'sqlite') {
            if (is_file($file) && ! str_contains(file_get_contents($file), '# DB_HOST=127.0.0.1')) {
                replaceInFile($file, $defaults, $commented);
            }
            continue;
        }
        replaceInFile($file, $commented, $defaults);
        $ports = ['pgsql' => '5432', 'sqlsrv' => '1433'];
        if (isset($ports[$db])) {
            replaceInFile($file, ['DB_PORT=3306'], ['DB_PORT='.$ports[$db]]);
        }
        replaceInFile($file, ['DB_DATABASE=laravel'], ['DB_DATABASE='.str_replace('-', '_', strtolower($name))]);
    }
}

// 1. The skeleton, the same way the installer fetches it.
step($kit ? "composer create-project {$kit}" : 'composer create-project laravel/laravel');
$create = $kit
    ? [...$composer, 'create-project', $kit, $name, '--stability=dev', '--no-interaction']
    : [...$composer, 'create-project', 'laravel/laravel', $name, '--remove-vcs', '--prefer-dist', '--no-scripts', '--no-interaction'];
if (($code = run($create, $parent)) !== 0) {
    exit($code);
}
if (run([...$composer, 'run', 'post-root-package-install', '-d', $dir], $parent) !== 0
    || run([$php, $dir.DIRECTORY_SEPARATOR.'artisan', 'key:generate', '--ansi'], $parent) !== 0) {
    exit(1);
}
if (PHP_OS_FAMILY !== 'Windows') {
    @chmod($dir.'/artisan', 0755);
}

// 2. Database connection in .env and .env.example.
step("Database: {$db}");
configureDatabase($dir, $db, $name);

// 3. Git repository with the fresh skeleton.
$git = haveGit();
if ($git) {
    step('git init');
    exec('git config --global init.defaultBranch', $branchOut);
    $branch = trim($branchOut[0] ?? '') ?: 'main';
    run(['git', 'init', '-q'], $dir);
    run(['git', 'add', '.'], $dir);
    run(['git', 'commit', '-q', '-m', 'Set up a fresh Laravel app'], $dir);
    run(['git', 'branch', '-M', $branch], $dir);
} else {
    warn('git not found: skipping the repository');
}

// 4. Pest instead of PHPUnit.
if ($pest) {
    step('Installing Pest');
    $env = ['PEST_NO_SUPPORT' => 'true'];
    $ok = run([...$composer, 'remove', 'phpunit/phpunit', '--dev', '--no-update'], $dir, $env) === 0
        && run([...$composer, 'require', 'pestphp/pest', 'pestphp/pest-plugin-laravel', '--no-update', '--dev'], $dir, $env) === 0
        && run([...$composer, 'update', '--no-interaction'], $dir, $env) === 0
        && run([$php, './vendor/bin/pest', '--init'], $dir, $env) === 0;
    if ($ok) {
        // Convert the skeleton's PHPUnit tests, as the installer does.
        if (run([...$composer, 'require', 'pestphp/pest-plugin-drift', '--dev', '--no-interaction'], $dir, $env) === 0) {
            run([$php, './vendor/bin/pest', '--drift'], $dir, $env);
            run([...$composer, 'remove', 'pestphp/pest-plugin-drift', '--dev', '--no-interaction'], $dir, $env);
        }
        if ($kit) {
            replaceInFile($dir.'/.github/workflows/tests.yml', ['./vendor/bin/phpunit'], ['./vendor/bin/pest']);
            replaceInFile($dir.'/tests/Pest.php',
                [" // ->use(Illuminate\\Foundation\\Testing\\RefreshDatabase::class)"],
                ["    ->use(Illuminate\\Foundation\\Testing\\RefreshDatabase::class)"]);
            $it = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($dir.'/tests', FilesystemIterator::SKIP_DOTS));
            foreach ($it as $file) {
                replaceInFile($file->getPathname(), ["\n\nuses(\\Illuminate\\Foundation\\Testing\\RefreshDatabase::class);"], ['']);
            }
        }
        if ($git) {
            run(['git', 'add', '.'], $dir);
            run(['git', 'commit', '-q', '-m', 'Install Pest'], $dir);
        }
    } else {
        warn('Pest could not be installed; the app keeps PHPUnit');
    }
}

// 5. Frontend assets for the starter kits.
if ($kit) {
    step('npm install && npm run build');
    if (run(['npm', 'install'], $dir) !== 0 || run(['npm', 'run', 'build'], $dir) !== 0) {
        warn('npm failed: run "npm install && npm run build" in the project later');
    }
}

fwrite(STDOUT, "\n\033[1;32m✓\033[0m Application ready in \033[1m{$dir}\033[0m\n");
exit(0);
