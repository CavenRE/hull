// Hull GUI — a thin Tauri shell over the hulld local API (ADR 0002).
// All business logic lives in the daemon; this process owns only the
// window, the tray icon, and daemon discovery.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use serde::Serialize;
use std::{fs, path::PathBuf};
use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    Manager,
};

#[derive(Serialize)]
struct DaemonInfo {
    port: u16,
    token: String,
}

fn hull_home() -> PathBuf {
    if let Ok(h) = std::env::var("HULL_HOME") {
        return PathBuf::from(h);
    }
    let home = std::env::var("USERPROFILE")
        .or_else(|_| std::env::var("HOME"))
        .unwrap_or_default();
    PathBuf::from(home).join(".hull")
}

/// Reads ~/.hull/daemon.json so the frontend can reach the API.
#[tauri::command]
fn daemon_info() -> Result<DaemonInfo, String> {
    let path = hull_home().join("daemon.json");
    let data = fs::read_to_string(&path)
        .map_err(|_| "daemon not running (no daemon.json) — start hulld".to_string())?;
    let v: serde_json::Value = serde_json::from_str(&data).map_err(|e| e.to_string())?;
    let port = v["port"].as_u64().unwrap_or(0) as u16;
    if port == 0 {
        return Err("corrupt daemon.json".into());
    }
    Ok(DaemonInfo {
        port,
        token: v["token"].as_str().unwrap_or_default().to_string(),
    })
}

/// True when Hull has never been set up on this machine (no config.yaml).
#[tauri::command]
fn first_run() -> bool {
    !hull_home().join("config.yaml").exists()
}

// ---- GUI startup preferences (~/.hull/gui.json) -------------------------
// Behaviours the GUI owns (not daemon config): close-to-tray, auto-start the
// daemon on launch, restore sites, update checks. Stored as a small JSON file
// so both this process (window close) and the frontend (Settings) agree.
fn prefs_path() -> PathBuf {
    hull_home().join("gui.json")
}

fn default_prefs() -> serde_json::Value {
    serde_json::json!({
        "close_to_tray": true,
        "start_daemon_on_launch": true,
        "restore_running": true,
        "check_updates": true,
    })
}

/// Reads gui.json, filling any missing keys with their defaults.
#[tauri::command]
fn get_gui_prefs() -> serde_json::Value {
    let def = default_prefs();
    let mut v = fs::read_to_string(prefs_path())
        .ok()
        .and_then(|s| serde_json::from_str::<serde_json::Value>(&s).ok())
        .unwrap_or_else(|| def.clone());
    if let (Some(obj), Some(dobj)) = (v.as_object_mut(), def.as_object()) {
        for (k, dv) in dobj {
            obj.entry(k.clone()).or_insert_with(|| dv.clone());
        }
    } else {
        v = def;
    }
    v
}

/// Persists a single boolean startup preference.
#[tauri::command]
fn set_gui_pref(key: String, value: bool) -> Result<(), String> {
    let mut v = get_gui_prefs();
    if let Some(obj) = v.as_object_mut() {
        obj.insert(key, serde_json::Value::Bool(value));
    }
    let home = hull_home();
    let _ = fs::create_dir_all(&home);
    let data = serde_json::to_string_pretty(&v).map_err(|e| e.to_string())?;
    fs::write(prefs_path(), data).map_err(|e| e.to_string())
}

fn close_to_tray() -> bool {
    get_gui_prefs()
        .get("close_to_tray")
        .and_then(|v| v.as_bool())
        .unwrap_or(true)
}

/// Spawns hulld: next to this exe (bundled), the dev tree, or PATH.
#[tauri::command]
fn start_daemon() -> Result<(), String> {
    let name = if cfg!(windows) { "hulld.exe" } else { "hulld" };
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let dir = exe
        .parent()
        .map(|p| p.to_path_buf())
        .unwrap_or_else(|| PathBuf::from("."));
    let candidates = [dir.join(name), dir.join("../../../../bin").join(name)];
    for c in candidates.iter() {
        if c.exists() {
            return spawn_detached(c.as_path());
        }
    }
    spawn_detached(std::path::Path::new(name))
        .map_err(|_| "hulld not found — start it from a terminal".to_string())
}

fn spawn_detached(path: &std::path::Path) -> Result<(), String> {
    let mut cmd = std::process::Command::new(path);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x0800_0000); // CREATE_NO_WINDOW
    }
    cmd.spawn().map(|_| ()).map_err(|e| e.to_string())
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            None,
        ))
        .invoke_handler(tauri::generate_handler![
            daemon_info,
            first_run,
            start_daemon,
            get_gui_prefs,
            set_gui_pref
        ])
        .setup(|app| {
            let show = MenuItem::with_id(app, "show", "Show Hull", true, None::<&str>)?;
            let quit = MenuItem::with_id(app, "quit", "Quit Hull", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show, &quit])?;
            TrayIconBuilder::with_id("hull-tray")
                .icon(app.default_window_icon().unwrap().clone())
                .tooltip("Hull")
                .menu(&menu)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "show" => {
                        if let Some(w) = app.get_webview_window("main") {
                            let _ = w.show();
                            let _ = w.unminimize();
                            let _ = w.set_focus();
                        }
                    }
                    "quit" => app.exit(0),
                    _ => {}
                })
                .build(app)?;
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                if close_to_tray() {
                    // Hiding the GUI never touches the daemon; reopen from the tray.
                    let _ = window.hide();
                    api.prevent_close();
                } else {
                    // Tray disabled: closing the window quits Hull's UI.
                    window.app_handle().exit(0);
                }
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running hull gui");
}
