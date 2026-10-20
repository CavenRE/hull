<?php
// Hull's Adminer plugin: a picker of Hull's databases on the login page, read
// from servers.json (Hull regenerates it whenever a database is added, linked,
// or removed). Clicking an entry fills the login form; Adminer's own login
// still needs the password, which is in the project's .env.
class AdminerHullLogin {
    function head() {
        $file = __DIR__ . '/servers.json';
        $servers = array();
        if (is_readable($file)) {
            $decoded = json_decode(file_get_contents($file), true);
            if (is_array($decoded)) {
                $servers = $decoded;
            }
        }
        $json = json_encode(array_values($servers));
        echo "<script>\nwindow.__hullDBs = $json;\n";
        echo <<<'JS'
document.addEventListener('DOMContentLoaded', function () {
  var servers = window.__hullDBs || [];
  if (!servers.length) return;
  var serverInp = document.querySelector('[name="auth[server]"]');
  if (!serverInp) return; // only render on the login page
  var form = serverInp.closest('form');
  if (!form) return;
  function setVal(name, val) {
    var el = form.querySelector('[name="' + name + '"]');
    if (el) { el.value = val; }
    return el;
  }
  var box = document.createElement('div');
  box.style.cssText = 'margin:1em 0;padding:.7em 1em;border:1px solid #d0d0d0;border-radius:6px;background:#fafafa;';
  var h = document.createElement('div');
  h.textContent = 'Hull databases (the password is DB_PASSWORD in the project .env)';
  h.style.cssText = 'font-weight:bold;margin-bottom:.4em;';
  box.appendChild(h);
  servers.forEach(function (s) {
    var a = document.createElement('a');
    a.href = '#';
    a.textContent = s.label + '  (' + s.engine + ')';
    a.style.cssText = 'display:block;padding:.2em 0;cursor:pointer;';
    a.addEventListener('click', function (e) {
      e.preventDefault();
      setVal('auth[driver]', (s.driver === 'pgsql') ? 'pgsql' : 'server');
      setVal('auth[server]', s.host + ':' + s.port);
      setVal('auth[username]', s.user);
      if (s.db) { setVal('auth[db]', s.db); }
      var pw = setVal('auth[password]', '');
      if (pw) { pw.focus(); }
    });
    box.appendChild(a);
  });
  form.parentNode.insertBefore(box, form);
});
JS;
        echo "\n</script>\n";
    }
}
return new AdminerHullLogin();
