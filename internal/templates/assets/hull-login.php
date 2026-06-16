<?php
// Hull's Adminer auto-login plugin (ported from v1): local dev databases
// have empty passwords, which Adminer refuses by default.
class AdminerHullLogin {
    function credentials() {
        return array($_GET["server"] ?? $_GET["mysql"] ?? $_GET["pgsql"] ?? "", $_GET["username"] ?? "", "");
    }
    function login($login, $password) {
        return true;
    }
}
return new AdminerHullLogin();
