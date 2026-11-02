<?php

/*
 * Larvyn bridge: boots a Laravel application and answers one request.
 * Usage: php bridge.php <project-path> <action>   (JSON payload on stdin)
 * The answer is printed after a marker so warnings during boot never break it.
 */

use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Schema;

const LARVYN_MARK = "\n@@LARVYN@@";

$base = $argv[1] ?? getcwd();
$action = $argv[2] ?? 'ping';
$payload = json_decode(stream_get_contents(STDIN) ?: '{}', true) ?: [];

function larvyn_reply(array $data): void
{
    echo LARVYN_MARK . json_encode($data, JSON_INVALID_UTF8_SUBSTITUTE | JSON_PARTIAL_OUTPUT_ON_ERROR);
    exit(0);
}

function larvyn_fail(Throwable $e): void
{
    larvyn_reply(['ok' => false, 'error' => [
        'class' => get_class($e),
        'message' => $e->getMessage(),
        'file' => $e->getFile(),
        'line' => $e->getLine(),
        'trace' => array_slice(array_map(fn ($f) => ($f['file'] ?? '?') . ':' . ($f['line'] ?? '?') . ' ' . ($f['class'] ?? '') . ($f['type'] ?? '') . ($f['function'] ?? ''), $e->getTrace()), 0, 12),
    ]]);
}

set_exception_handler('larvyn_fail');


function larvyn_value($v)
{
    if (is_resource($v)) {
        return '(resource)';
    }
    if (is_string($v) && !mb_check_encoding($v, 'UTF-8')) {
        return '0x' . bin2hex(strlen($v) > 64 ? substr($v, 0, 64) : $v) . (strlen($v) > 64 ? '…' : '');
    }
    return $v;
}

function larvyn_rows(iterable $rows): array
{
    $out = [];
    foreach ($rows as $row) {
        $out[] = array_map('larvyn_value', (array) $row);
    }
    return $out;
}

function larvyn_tables(): array
{
    $conn = DB::connection();
    if (method_exists(Schema::getFacadeRoot(), 'getTables')) {
        $tables = Schema::getTables();
        $schemaName = method_exists($conn, 'getDatabaseName') ? $conn->getDatabaseName() : null;
        return array_values(array_map(fn ($t) => [
            'name' => $t['name'],
            'schema' => $t['schema'] ?? null,
            'size' => $t['size'] ?? null,
            'comment' => $t['comment'] ?? null,
            'engine' => $t['engine'] ?? null,
        ], array_filter($tables, fn ($t) => !isset($t['schema']) || !$schemaName || in_array($t['schema'], [$schemaName, 'public', 'main', 'dbo'], true))));
    }
    // Laravel 10 and older.
    return array_map(fn ($n) => ['name' => $n, 'size' => null], $conn->getDoctrineSchemaManager()->listTableNames());
}

function larvyn_quote_ident(string $name): string
{
    return DB::connection()->getQueryGrammar()->wrapTable($name);
}

// Laravel installs its own exception handler while booting, so failures are
// caught here rather than left to it.
try {
chdir($base);
require $base . '/vendor/autoload.php';
$app = require $base . '/bootstrap/app.php';
$kernel = $app->make(Illuminate\Contracts\Console\Kernel::class);
$kernel->bootstrap();

$t0 = microtime(true);

switch ($action) {
    case 'ping':
        larvyn_reply(['ok' => true, 'laravel' => $app->version(), 'php' => PHP_VERSION, 'env' => $app->environment()]);

    case 'db.overview':
        $conn = DB::connection();
        $tables = larvyn_tables();
        foreach ($tables as &$t) {
            try {
                $t['rows'] = DB::table($t['name'])->count();
            } catch (Throwable $e) {
                $t['rows'] = null;
            }
        }
        larvyn_reply([
            'ok' => true,
            'driver' => $conn->getDriverName(),
            'database' => $conn->getDatabaseName(),
            'connection' => $conn->getName(),
            'version' => rescue(fn () => method_exists($conn, 'getServerVersion') ? $conn->getServerVersion() : $conn->getPdo()->getAttribute(PDO::ATTR_SERVER_VERSION), null, false),
            'tables' => $tables,
        ]);

    case 'db.table':
        $table = $payload['table'];
        $columns = method_exists(Schema::getFacadeRoot(), 'getColumns') ? Schema::getColumns($table) : array_map(fn ($c) => [
            'name' => $c->getName(), 'type_name' => $c->getType()->getName(), 'type' => $c->getType()->getName(),
            'nullable' => !$c->getNotnull(), 'default' => $c->getDefault(), 'auto_increment' => $c->getAutoincrement(), 'comment' => $c->getComment(),
        ], DB::connection()->getDoctrineSchemaManager()->listTableColumns($table));
        $indexes = method_exists(Schema::getFacadeRoot(), 'getIndexes') ? Schema::getIndexes($table) : [];
        $foreign = method_exists(Schema::getFacadeRoot(), 'getForeignKeys') ? Schema::getForeignKeys($table) : [];
        larvyn_reply(['ok' => true, 'columns' => array_values($columns), 'indexes' => array_values($indexes), 'foreignKeys' => array_values($foreign)]);

    case 'db.rows':
        $table = $payload['table'];
        $page = max(1, (int) ($payload['page'] ?? 1));
        $per = min(500, max(1, (int) ($payload['perPage'] ?? 50)));
        $q = DB::table($table);
        $cols = Schema::getColumnListing($table);
        if (!empty($payload['search'])) {
            $term = '%' . $payload['search'] . '%';
            $q->where(function ($w) use ($cols, $term) {
                foreach ($cols as $c) {
                    $w->orWhere($c, 'like', $term);
                }
            });
        }
        foreach (($payload['filters'] ?? []) as $f) {
            if (in_array($f['column'] ?? null, $cols, true)) {
                $op = in_array($f['op'] ?? '=', ['=', '!=', '<', '>', '<=', '>=', 'like'], true) ? $f['op'] : '=';
                $f['value'] === null ? $q->whereNull($f['column']) : $q->where($f['column'], $op, $f['value']);
            }
        }
        $total = (clone $q)->count();
        if (!empty($payload['orderBy']) && in_array($payload['orderBy'], $cols, true)) {
            $q->orderBy($payload['orderBy'], ($payload['direction'] ?? 'asc') === 'desc' ? 'desc' : 'asc');
        }
        $rows = $q->forPage($page, $per)->get();
        larvyn_reply(['ok' => true, 'columns' => $cols, 'rows' => larvyn_rows($rows), 'total' => $total, 'page' => $page, 'perPage' => $per, 'ms' => round((microtime(true) - $t0) * 1000, 1)]);

    case 'db.query':
        $sql = trim($payload['sql'] ?? '');
        $isRead = preg_match('/^\s*(select|with|show|describe|desc|explain|pragma|values)\b/i', $sql);
        if ($isRead) {
            $rows = DB::select($sql);
            $limited = array_slice($rows, 0, 1000);
            $cols = $limited ? array_keys((array) $limited[0]) : [];
            larvyn_reply(['ok' => true, 'kind' => 'rows', 'columns' => $cols, 'rows' => larvyn_rows($limited), 'total' => count($rows), 'truncated' => count($rows) > 1000, 'ms' => round((microtime(true) - $t0) * 1000, 1)]);
        }
        $affected = DB::affectingStatement($sql);
        larvyn_reply(['ok' => true, 'kind' => 'affected', 'affected' => $affected, 'ms' => round((microtime(true) - $t0) * 1000, 1)]);

    case 'db.update':
        $table = $payload['table'];
        $affected = DB::table($table)->where($payload['key'], $payload['id'])->update($payload['values']);
        larvyn_reply(['ok' => true, 'affected' => $affected]);

    case 'db.delete':
        $affected = DB::table($payload['table'])->where($payload['key'], $payload['id'])->delete();
        larvyn_reply(['ok' => true, 'affected' => $affected]);

    case 'tinker':
        $code = trim($payload['code'] ?? '');
        $code = preg_replace('/^<\?php\s*/', '', $code);
        // Return the last expression, the way Tinker does.
        $lines = preg_split('/;\s*(?=(?:[^\'"]|\'[^\']*\'|"[^"]*")*$)/', rtrim($code, "; \n\r\t"));
        $last = array_pop($lines);
        $prefix = $lines ? implode(";\n", $lines) . ";\n" : '';
        $isStatement = preg_match('/^\s*(return|echo|print|if|foreach|for|while|function|class|use|throw|unset|\$\w+\s*=[^=>])/', (string) $last);
        $body = $prefix . ($isStatement ? $last . ';' : 'return ' . $last . ';');
        DB::enableQueryLog();
        ob_start();
        try {
            $result = (function () use ($body) {
                return eval($body);
            })();
        } catch (Throwable $e) {
            $output = ob_get_clean();
            larvyn_reply(['ok' => false, 'output' => $output, 'error' => ['class' => get_class($e), 'message' => $e->getMessage(), 'file' => $e->getFile(), 'line' => $e->getLine()], 'queries' => DB::getQueryLog()]);
        }
        $output = ob_get_clean();
        $type = get_debug_type($result);
        if (class_exists(\Symfony\Component\VarDumper\Dumper\CliDumper::class)) {
            $cloner = new \Symfony\Component\VarDumper\Cloner\VarCloner();
            $cloner->setMaxItems(200);
            $dumper = new \Symfony\Component\VarDumper\Dumper\CliDumper();
            $dumper->setColors(false);
            $dump = $dumper->dump($cloner->cloneVar($result), true);
        } else {
            $dump = var_export($result, true);
        }
        $json = null;
        try {
            if ($result instanceof \Illuminate\Contracts\Support\Arrayable || is_array($result) || $result instanceof \JsonSerializable) {
                $json = json_decode(json_encode($result, JSON_INVALID_UTF8_SUBSTITUTE | JSON_PARTIAL_OUTPUT_ON_ERROR), true);
            }
        } catch (Throwable $e) {
        }
        larvyn_reply([
            'ok' => true,
            'output' => $output,
            'dump' => $dump,
            'type' => $type,
            'json' => $json,
            'queries' => array_map(fn ($q) => ['sql' => $q['query'], 'bindings' => array_map('larvyn_value', $q['bindings']), 'time' => $q['time']], DB::getQueryLog()),
            'ms' => round((microtime(true) - $t0) * 1000, 1),
            'memory' => memory_get_peak_usage(true),
        ]);

    case 'models':
        $models = [];
        $dir = app_path('Models');
        $files = is_dir($dir) ? new RecursiveIteratorIterator(new RecursiveDirectoryIterator($dir)) : [];
        foreach ($files as $file) {
            if ($file->getExtension() !== 'php') {
                continue;
            }
            $rel = str_replace(['/', '.php'], ['\\', ''], substr($file->getPathname(), strlen($dir) + 1));
            $class = 'App\\Models\\' . str_replace('/', '\\', $rel);
            $class = str_replace('\\\\', '\\', $class);
            try {
                if (!class_exists($class)) {
                    continue;
                }
                $ref = new ReflectionClass($class);
                if ($ref->isAbstract() || !$ref->isSubclassOf(\Illuminate\Database\Eloquent\Model::class)) {
                    continue;
                }
                $m = $ref->newInstanceWithoutConstructor();
                $relations = [];
                foreach ($ref->getMethods(ReflectionMethod::IS_PUBLIC) as $method) {
                    if ($method->getNumberOfParameters() > 0 || $method->class !== $class) {
                        continue;
                    }
                    $ret = $method->getReturnType();
                    $retName = $ret instanceof ReflectionNamedType ? $ret->getName() : null;
                    if ($retName && is_subclass_of($retName, \Illuminate\Database\Eloquent\Relations\Relation::class)) {
                        $related = null;
                        try {
                            $related = get_class((new $class)->{$method->name}()->getRelated());
                        } catch (Throwable $e) {
                        }
                        $relations[] = ['name' => $method->name, 'type' => class_basename($retName), 'related' => $related];
                    }
                }
                $instance = new $class;
                $count = null;
                try {
                    $count = $class::query()->toBase()->getCountForPagination();
                } catch (Throwable $e) {
                }
                $models[] = [
                    'class' => $class,
                    'name' => class_basename($class),
                    'file' => $file->getPathname(),
                    'table' => $instance->getTable(),
                    'connection' => $instance->getConnectionName(),
                    'primaryKey' => $instance->getKeyName(),
                    'fillable' => $instance->getFillable(),
                    'guarded' => $instance->getGuarded(),
                    'hidden' => $instance->getHidden(),
                    'casts' => $instance->getCasts(),
                    'appends' => method_exists($instance, 'getAppends') ? $instance->getAppends() : [],
                    'timestamps' => $instance->usesTimestamps(),
                    'softDeletes' => in_array(\Illuminate\Database\Eloquent\SoftDeletes::class, class_uses_recursive($class), true),
                    // Eloquent's own concerns are on every model; show the ones the app chose.
                    'traits' => array_values(array_map('class_basename', array_filter(class_uses_recursive($class), fn ($t) => !str_starts_with($t, 'Illuminate\Database\Eloquent\Concerns') && !str_starts_with($t, 'Illuminate\Support\Traits')))),
                    'relations' => $relations,
                    'count' => $count,
                ];
            } catch (Throwable $e) {
                $models[] = ['class' => $class, 'name' => class_basename($class), 'file' => $file->getPathname(), 'error' => $e->getMessage()];
            }
        }
        usort($models, fn ($a, $b) => strcmp($a['name'], $b['name']));
        larvyn_reply(['ok' => true, 'models' => $models]);

    case 'migrations':
        $migrator = app('migrator');
        $paths = array_merge([database_path('migrations')], $migrator->paths());
        $files = $migrator->getMigrationFiles($paths);
        $repo = $migrator->getRepository();
        $ran = [];
        $batches = [];
        $repoExists = $repo->repositoryExists();
        if ($repoExists) {
            $ran = $repo->getRan();
            $batches = $repo->getMigrationBatches();
        }
        $out = [];
        foreach ($files as $name => $path) {
            $out[] = ['name' => $name, 'path' => $path, 'ran' => in_array($name, $ran, true), 'batch' => $batches[$name] ?? null];
        }
        larvyn_reply(['ok' => true, 'repository' => $repoExists, 'migrations' => $out]);

    case 'queue.failed':
        $failer = app('queue.failer');
        $jobs = array_map(function ($j) {
            $j = (array) $j;
            $payload = json_decode($j['payload'] ?? '{}', true);
            return [
                'id' => $j['uuid'] ?? $j['id'],
                'connection' => $j['connection'] ?? null,
                'queue' => $j['queue'] ?? null,
                'name' => $payload['displayName'] ?? null,
                'exception' => mb_substr($j['exception'] ?? '', 0, 4000),
                'failedAt' => $j['failed_at'] ?? null,
            ];
        }, $failer->all());
        $pending = null;
        try {
            if (config('queue.default') === 'database') {
                $pending = DB::table(config('queue.connections.database.table', 'jobs'))->count();
            }
        } catch (Throwable $e) {
        }
        larvyn_reply(['ok' => true, 'failed' => $jobs, 'pending' => $pending, 'default' => config('queue.default')]);

    case 'config':
        $key = $payload['key'] ?? null;
        $value = $key ? config($key) : array_keys(config()->all());
        larvyn_reply(['ok' => true, 'value' => json_decode(json_encode($value, JSON_PARTIAL_OUTPUT_ON_ERROR), true)]);

    case 'schedule':
        $schedule = app(\Illuminate\Console\Scheduling\Schedule::class);
        $events = array_map(fn ($e) => [
            'command' => $e->command ? preg_replace('/^.*artisan[\'"]?\s/', 'artisan ', $e->command) : ($e->description ?: 'Closure'),
            'expression' => $e->expression,
            'description' => $e->description,
            'next' => rescue(fn () => $e->nextRunDate()->format('Y-m-d H:i:s'), null, false),
            'timezone' => is_string($e->timezone) ? $e->timezone : (string) ($e->timezone?->getName() ?? config('app.timezone')),
            'withoutOverlapping' => $e->withoutOverlapping,
            'onOneServer' => $e->onOneServer,
        ], $schedule->events());
        larvyn_reply(['ok' => true, 'events' => $events]);

    default:
        larvyn_reply(['ok' => false, 'error' => ['class' => 'Larvyn', 'message' => "Unknown action $action"]]);
}
} catch (Throwable $e) {
    larvyn_fail($e);
}
