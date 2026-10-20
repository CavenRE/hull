<?php
// Managed by Hull. Runs inside the web server's PHP (PHP-FPM or Apache with
// mod_php), because that is where the opcode cache lives: the PHP CLI keeps a
// separate one, so Hull's daemon reaches this file through hull-opcache-cli.php.
// The body lists one container path per line; ?mode= picks the operation.

if (!function_exists('opcache_invalidate') || !ini_get('opcache.enable')) {
    http_response_code(503);
    echo "hull-opcache: OPcache is not enabled\n";
    return;
}

$paths = preg_split('/\r?\n/', (string) file_get_contents('php://input'), -1, PREG_SPLIT_NO_EMPTY);
$mode = isset($_GET['mode']) ? $_GET['mode'] : '';

if ($mode === 'invalidate') {
    $n = 0;
    foreach ($paths as $path) {
        if (opcache_invalidate($path, true)) {
            $n++;
        }
    }
    echo "hull-opcache: ok invalidated=$n\n";
} elseif ($mode === 'reset') {
    echo opcache_reset() ? "hull-opcache: ok reset\n" : "hull-opcache: reset refused\n";
} elseif ($mode === 'compile') {
    set_time_limit(0);
    ignore_user_abort(true);
    // A reset only lands once no request is running; compiling before that
    // would fill the cache that is about to be thrown away.
    for ($i = 0; $i < 50; $i++) {
        $status = opcache_get_status(false);
        if (empty($status['restart_pending']) && empty($status['restart_in_progress'])) {
            break;
        }
        usleep(200000);
    }
    $compiled = 0;
    // A file that does not compile can end this request with a fatal error;
    // report what was done so far, so the caller moves on to its next batch.
    register_shutdown_function(function () use (&$compiled) {
        echo "hull-opcache: ok compiled=$compiled\n";
    });
    foreach ($paths as $i => $path) {
        if ($i % 200 === 0) {
            $status = opcache_get_status(false);
            if (!empty($status['cache_full'])) {
                break;
            }
        }
        if (opcache_is_script_cached($path)) {
            continue;
        }
        try {
            if (@opcache_compile_file($path)) {
                $compiled++;
            }
        } catch (\Throwable $e) {
        }
    }
} else {
    http_response_code(400);
    echo "hull-opcache: unknown mode\n";
}
