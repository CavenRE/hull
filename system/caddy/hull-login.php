<?php
class AdminerHullLogin {
    function credentials() {
        return array($_GET["server"] ?? $_GET["mysql"] ?? $_GET["pgsql"], $_GET["username"], "");
    }
    function login($login, $password) {
        return true;
    }
}
return new AdminerHullLogin();
