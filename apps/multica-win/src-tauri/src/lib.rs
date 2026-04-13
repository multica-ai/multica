mod config;
mod daemon;
mod proxy;

use std::sync::atomic::{AtomicBool, Ordering};
use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    Manager,
};

static LAST_TRAY_CLICK: AtomicBool = AtomicBool::new(false);

#[tauri::command]
fn get_config() -> Result<config::AppConfig, String> {
    config::AppConfig::load()
}

#[tauri::command]
fn save_config(config: config::AppConfig) -> Result<(), String> {
    config.save()
}

#[tauri::command]
fn cmd_open_folder(path: String) -> Result<(), String> {
    let mut final_path = std::path::PathBuf::from(&path);
    
    if path.starts_with("~/") {
        if let Some(home) = dirs::home_dir() {
            final_path = home.join(&path[2..]);
        }
    } else if path == "~" {
        if let Some(home) = dirs::home_dir() {
            final_path = home;
        }
    }

    // Create the directory if it doesn't exist
    if !final_path.exists() {
        let _ = std::fs::create_dir_all(&final_path);
    }

    #[cfg(target_os = "windows")]
    {
        std::process::Command::new("explorer")
            .arg(final_path.to_str().unwrap_or(&path))
            .spawn()
            .map_err(|e| e.to_string())?;
    }
    #[cfg(target_os = "macos")]
    {
        std::process::Command::new("open")
            .arg(&path)
            .spawn()
            .map_err(|e| e.to_string())?;
    }
    #[cfg(target_os = "linux")]
    {
        std::process::Command::new("xdg-open")
            .arg(&path)
            .spawn()
            .map_err(|e| e.to_string())?;
    }
    Ok(())
}

#[tauri::command]
fn quit_app(app_handle: tauri::AppHandle) {
    let _ = daemon::stop_daemon();
    app_handle.exit(0);
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .setup(|app| {
            let show_item = MenuItem::with_id(app, "show", "Show Multica", true, None::<&str>)?;
            let quit_item = MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show_item, &quit_item])?;

            let _tray = TrayIconBuilder::new()
                .menu(&menu)
                .tooltip("Multica")
                .icon(app.default_window_icon().unwrap().clone())
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "show" => {
                        if let Some(w) = app.get_webview_window("main") {
                            let _ = w.show();
                            let _ = w.set_focus();
                        }
                    }
                    "quit" => {
                        let _ = daemon::stop_daemon();
                        app.exit(0);
                    }
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let tauri::tray::TrayIconEvent::Click { .. } = event {
                        // Debounce: ignore if clicked in last 500ms
                        if LAST_TRAY_CLICK.swap(true, Ordering::SeqCst) {
                            return;
                        }
                        std::thread::spawn(|| {
                            std::thread::sleep(std::time::Duration::from_millis(500));
                            LAST_TRAY_CLICK.store(false, Ordering::SeqCst);
                        });

                        let app = tray.app_handle();
                        if let Some(w) = app.get_webview_window("main") {
                            if w.is_visible().unwrap_or(false) {
                                let _ = w.hide();
                            } else {
                                let _ = w.show();
                                let _ = w.set_focus();
                            }
                        }
                    }
                })
                .build(app)?;

            // Close → hide to tray
            if let Some(window) = app.get_webview_window("main") {
                let win = window.clone();
                window.on_window_event(move |event| {
                    if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                        api.prevent_close();
                        let _ = win.hide();
                    }
                });
            }

            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            get_config,
            save_config,
            cmd_open_folder,
            quit_app,
            daemon::start_daemon,
            daemon::stop_daemon,
            daemon::daemon_status,
            proxy::check_health,
            proxy::get_current_user,
            proxy::get_workspaces,
            proxy::get_runtimes,
            proxy::get_agents,
            proxy::get_issues,
            proxy::get_inbox,
            proxy::get_token_usage,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
