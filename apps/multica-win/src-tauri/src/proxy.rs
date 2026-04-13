use reqwest::Client;
use serde_json::Value;
use std::time::{Duration, Instant};
use std::sync::{OnceLock, Mutex};
use crate::config::AppConfig;

static CLIENT: OnceLock<Client> = OnceLock::new();
static CONFIG_CACHE: OnceLock<Mutex<(Option<AppConfig>, Instant)>> = OnceLock::new();

fn get_client() -> &'static Client {
    CLIENT.get_or_init(|| {
        Client::builder()
            .timeout(Duration::from_secs(10))
            .connect_timeout(Duration::from_secs(5))
            .tcp_keepalive(Some(Duration::from_secs(60)))
            .pool_idle_timeout(Duration::from_secs(90))
            .build()
            .unwrap_or_default()
    })
}

fn get_cached_config() -> AppConfig {
    let cache = CONFIG_CACHE.get_or_init(|| Mutex::new((None, Instant::now() - Duration::from_secs(60))));
    let mut lock = cache.lock().unwrap();
    
    // Cache for 5 seconds to avoid excessive disk reads during rapid API calls
    if lock.0.is_none() || lock.1.elapsed() > Duration::from_secs(5) {
        if let Ok(config) = AppConfig::load() {
            lock.0 = Some(config);
            lock.1 = Instant::now();
        }
    }
    
    lock.0.clone().unwrap_or_else(|| AppConfig {
        server_url: "http://localhost:8080".to_string(),
        app_url: None,
        token: None,
        workspace_id: None,
        watched_workspaces: None,
    })
}

async fn proxy_get(path: &str, workspace_id: Option<&str>) -> Result<Value, String> {
    let config = get_cached_config();
    let client = get_client();
    let url = format!("{}/{}", config.server_url.trim_end_matches('/'), path.trim_start_matches('/'));

    let mut req = client.get(&url);
    if let Some(token) = &config.token {
        req = req.header("Authorization", format!("Bearer {}", token));
    }
    if let Some(ws_id) = workspace_id {
        req = req.header("X-Workspace-ID", ws_id);
    }

    let resp = req.send().await.map_err(|e| format!("Network error: {}", e))?;
    if !resp.status().is_success() {
        return Err(format!("Server returned HTTP {}", resp.status()));
    }
    resp.json::<Value>().await.map_err(|e| format!("JSON parse error: {}", e))
}

// === Tauri Commands ===

#[tauri::command]
pub async fn check_health() -> Result<Value, String> {
    proxy_get("health", None).await
}

#[tauri::command]
pub async fn get_current_user() -> Result<Value, String> {
    proxy_get("api/me", None).await
}

#[tauri::command]
pub async fn get_workspaces() -> Result<Value, String> {
    proxy_get("api/workspaces", None).await
}

#[tauri::command]
pub async fn get_runtimes(workspace_id: String) -> Result<Value, String> {
    let path = format!("api/runtimes?workspace_id={}", workspace_id);
    proxy_get(&path, Some(&workspace_id)).await
}

#[tauri::command]
pub async fn get_agents(workspace_id: String) -> Result<Value, String> {
    proxy_get("api/agents", Some(&workspace_id)).await
}

#[tauri::command]
pub async fn get_issues(workspace_id: String) -> Result<Value, String> {
    proxy_get("api/issues/", Some(&workspace_id)).await
}

#[tauri::command]
pub async fn get_inbox(workspace_id: String) -> Result<Value, String> {
    proxy_get("api/inbox", Some(&workspace_id)).await
}

#[tauri::command]
pub async fn get_token_usage(workspace_id: String) -> Result<Value, String> {
    proxy_get("api/analytics/token-usage", Some(&workspace_id)).await
}
