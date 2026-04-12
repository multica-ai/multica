use reqwest::Client;
use serde_json::Value;
use std::time::Duration;

fn build_client() -> Client {
    Client::builder()
        .timeout(Duration::from_secs(5))
        .connect_timeout(Duration::from_secs(3))
        .build()
        .unwrap_or_default()
}

fn server_url() -> String {
    crate::config::AppConfig::load()
        .map(|c| c.server_url)
        .unwrap_or_else(|_| "http://localhost:8080".to_string())
}

fn auth_token() -> Option<String> {
    crate::config::AppConfig::load().ok().and_then(|c| c.token)
}

async fn proxy_get(path: &str, workspace_id: Option<&str>) -> Result<Value, String> {
    let client = build_client();
    let url = format!("{}/{}", server_url(), path.trim_start_matches('/'));

    let mut req = client.get(&url);
    if let Some(token) = auth_token() {
        req = req.header("Authorization", format!("Bearer {}", token));
    }
    if let Some(ws_id) = workspace_id {
        req = req.header("X-Workspace-ID", ws_id);
    }

    let resp = req.send().await.map_err(|e| format!("{}", e))?;
    if !resp.status().is_success() {
        return Err(format!("HTTP {}", resp.status()));
    }
    resp.json::<Value>().await.map_err(|e| format!("{}", e))
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
