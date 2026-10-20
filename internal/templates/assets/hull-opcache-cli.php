<?php
// Managed by Hull. Hull's daemon runs this with the container's PHP CLI to
// reach the opcode cache of the web server's PHP, which the CLI does not share:
// over HTTP to Apache (mod_php, the wordpress image) or over FastCGI to
// PHP-FPM (serversideup). Usage: php hull-opcache-cli.php MODE < paths
// MODE is invalidate, reset or compile; stdin lists one container path per line.

const HULL_HELPER = '/opt/hull/hull-opcache.php';

$mode = isset($argv[1]) ? $argv[1] : '';
$paths = preg_split('/\r?\n/', (string) stream_get_contents(STDIN), -1, PREG_SPLIT_NO_EMPTY);
// A file that does not compile can end a compile request early, so the list
// goes in batches and one bad file costs only the rest of its batch.
$batches = $mode === 'compile' ? array_chunk($paths, 250) : array($paths);
$viaApache = getenv('APACHE_CONFDIR') !== false;

foreach ($batches as $batch) {
    $body = implode("\n", $batch);
    try {
        $reply = $viaApache ? hull_via_apache($mode, $body) : hull_via_fpm($mode, $body);
    } catch (Exception $e) {
        fwrite(STDERR, 'hull-opcache: ' . $e->getMessage() . "\n");
        exit(1);
    }
    echo $reply;
    if (strpos($reply, 'hull-opcache: ok') === false) {
        fwrite(STDERR, trim($reply) . "\n");
        exit(1);
    }
}

function hull_via_apache($mode, $body)
{
    $context = stream_context_create(array('http' => array(
        'method' => 'POST',
        'header' => "Content-Type: text/plain\r\n",
        'content' => $body,
        'timeout' => 600,
        'ignore_errors' => true,
    )));
    $reply = @file_get_contents('http://127.0.0.1/__hull/opcache?mode=' . rawurlencode($mode), false, $context);
    if ($reply === false) {
        throw new Exception('cannot reach Apache on 127.0.0.1:80');
    }
    return $reply;
}

function hull_via_fpm($mode, $body)
{
    $query = 'mode=' . rawurlencode($mode);
    $params = array(
        'GATEWAY_INTERFACE' => 'CGI/1.1',
        'SERVER_SOFTWARE' => 'hull',
        'SERVER_PROTOCOL' => 'HTTP/1.1',
        'SERVER_NAME' => 'localhost',
        'SERVER_PORT' => '80',
        'REMOTE_ADDR' => '127.0.0.1',
        'REQUEST_METHOD' => 'POST',
        'REQUEST_URI' => '/hull-opcache.php?' . $query,
        'QUERY_STRING' => $query,
        'SCRIPT_NAME' => '/hull-opcache.php',
        'SCRIPT_FILENAME' => HULL_HELPER,
        'DOCUMENT_ROOT' => dirname(HULL_HELPER),
        'CONTENT_TYPE' => 'text/plain',
        'CONTENT_LENGTH' => (string) strlen($body),
    );
    $pairs = '';
    foreach ($params as $name => $value) {
        $pairs .= hull_fcgi_length(strlen($name)) . hull_fcgi_length(strlen($value)) . $name . $value;
    }
    $request = hull_fcgi_record(1, pack('nCx5', 1, 0))
        . hull_fcgi_stream(4, $pairs)
        . hull_fcgi_stream(5, $body);

    $socket = hull_fpm_connect();
    stream_set_timeout($socket, 600);
    for ($sent = 0; $sent < strlen($request); $sent += $n) {
        $n = fwrite($socket, substr($request, $sent));
        if ($n === false || $n === 0) {
            throw new Exception('PHP-FPM closed the connection');
        }
    }
    $stdout = '';
    while (true) {
        $header = unpack('Cversion/Ctype/nid/nlength/Cpadding/Creserved', hull_read($socket, 8));
        $content = $header['length'] > 0 ? hull_read($socket, $header['length']) : '';
        if ($header['padding'] > 0) {
            hull_read($socket, $header['padding']);
        }
        if ($header['type'] === 6) {
            $stdout .= $content;
        } elseif ($header['type'] === 7) {
            fwrite(STDERR, $content);
        } elseif ($header['type'] === 3) {
            break;
        }
    }
    fclose($socket);
    $split = strpos($stdout, "\r\n\r\n");
    return $split === false ? $stdout : substr($stdout, $split + 4);
}

// hull_fpm_connect tries the address every image built on the official PHP-FPM
// image uses, then asks php-fpm where this pool listens.
function hull_fpm_connect()
{
    $targets = array('tcp://127.0.0.1:9000');
    $config = function_exists('shell_exec') ? (string) @shell_exec('php-fpm -tt 2>&1') : '';
    if (preg_match('/\blisten = (\S+)/', $config, $m)) {
        $listen = $m[1];
        if ($listen[0] === '/') {
            $targets[] = 'unix://' . $listen;
        } else {
            $port = substr($listen, strrpos($listen, ':') === false ? 0 : strrpos($listen, ':') + 1);
            $targets[] = 'tcp://127.0.0.1:' . $port;
        }
    }
    foreach ($targets as $target) {
        $socket = @stream_socket_client($target, $errno, $error, 5);
        if ($socket !== false) {
            return $socket;
        }
    }
    throw new Exception('cannot reach PHP-FPM (tried ' . implode(', ', $targets) . ')');
}

function hull_fcgi_record($type, $content)
{
    return pack('CCnnCx', 1, $type, 1, strlen($content), 0) . $content;
}

function hull_fcgi_stream($type, $data)
{
    $out = '';
    for ($i = 0; $i < strlen($data); $i += 65535) {
        $out .= hull_fcgi_record($type, substr($data, $i, 65535));
    }
    return $out . hull_fcgi_record($type, '');
}

function hull_fcgi_length($n)
{
    return $n < 128 ? chr($n) : pack('N', $n | 0x80000000);
}

function hull_read($socket, $n)
{
    $buf = '';
    while (strlen($buf) < $n) {
        $chunk = fread($socket, $n - strlen($buf));
        if ($chunk === false || $chunk === '') {
            $meta = stream_get_meta_data($socket);
            throw new Exception($meta['timed_out'] ? 'PHP-FPM timed out' : 'PHP-FPM closed the connection');
        }
        $buf .= $chunk;
    }
    return $buf;
}
