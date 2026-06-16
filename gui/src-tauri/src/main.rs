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
        .invoke_handler(tauri::generate_handler![daemon_info, first_run, start_daemon])
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
                // Close-to-tray: hiding the GUI never touches the daemon.
                let _ = window.hide();
                api.prevent_close();
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running hull gui");
}
