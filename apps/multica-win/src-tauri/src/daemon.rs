use serde::Serialize;
use std::os::windows::process::CommandExt;
use std::process::Command;

const CREATE_NO_WINDOW: u32 = 0x08000000;

#[derive(Debug, Clone, Serialize)]
pub struct DaemonStatus {
    pub running: bool,
    pub pid: Option<u32>,
    pub log_lines: Vec<String>,
}

fn strip_ansi(s: &str) -> String {
    let re = regex::Regex::new(r"\x1b\[[0-9;]*[A-Za-z]").unwrap();
    re.replace_all(s, "").to_string()
}

fn read_daemon_log(max_lines: usize) -> Vec<String> {
    let home = match dirs::home_dir() {
        Some(h) => h,
        None => return vec!["(cannot find home directory)".to_string()],
    };
    let log_path = home.join(".multica").join("daemon.log");
    let content = match std::fs::read_to_string(&log_path) {
        Ok(c) => c,
        Err(_) => return vec!["(no daemon log found)".to_string()],
    };
    let lines: Vec<String> = content.lines().map(|l| strip_ansi(l)).collect();
    let start = if lines.len() > max_lines { lines.len() - max_lines } else { 0 };
    lines[start..].to_vec()
}

fn read_pid_file() -> Option<u32> {
    let home = dirs::home_dir()?;
    let pid_path = home.join(".multica").join("daemon.pid");
    let content = std::fs::read_to_string(pid_path).ok()?;
    content.trim().parse::<u32>().ok()
}

fn is_pid_running(pid: u32) -> bool {
    let output = Command::new("tasklist")
        .args(["/FI", &format!("PID eq {}", pid), "/FO", "CSV", "/NH"])
        .creation_flags(CREATE_NO_WINDOW)
        .output();
    match output {
        Ok(out) => {
            let stdout = String::from_utf8_lossy(&out.stdout);
            stdout.contains(&pid.to_string())
        }
        Err(_) => false,
    }
}

fn get_daemon_pid() -> Option<u32> {
    let pid = read_pid_file()?;
    if is_pid_running(pid) { Some(pid) } else { None }
}

#[tauri::command]
pub fn start_daemon() -> Result<String, String> {
    if get_daemon_pid().is_some() {
        return Err("Daemon is already running".to_string());
    }

    let exe_path = crate::config::AppConfig::multica_exe_path()?;

    let child = Command::new(&exe_path)
        .args(["daemon", "start"])
        .creation_flags(CREATE_NO_WINDOW)
        .spawn()
        .map_err(|e| format!("Failed to start daemon: {}", e))?;

    Ok(format!("Daemon start triggered (PID: {})", child.id()))
}

#[tauri::command]
pub fn stop_daemon() -> Result<String, String> {
    let pid = match get_daemon_pid() {
        Some(p) => p,
        None => return Err("Daemon is not running".to_string()),
    };

    let output = Command::new("taskkill")
        .args(["/F", "/PID", &pid.to_string()])
        .creation_flags(CREATE_NO_WINDOW)
        .output()
        .map_err(|e| format!("Failed to stop: {}", e))?;

    if output.status.success() {
        if let Some(home) = dirs::home_dir() {
            let _ = std::fs::remove_file(home.join(".multica").join("daemon.pid"));
        }
        Ok("Daemon stopped".to_string())
    } else {
        Err(format!("Failed to stop PID {}", pid))
    }
}

#[tauri::command]
pub fn daemon_status() -> Result<DaemonStatus, String> {
    let pid = get_daemon_pid();
    Ok(DaemonStatus {
        running: pid.is_some(),
        pid,
        log_lines: read_daemon_log(50),
    })
}
