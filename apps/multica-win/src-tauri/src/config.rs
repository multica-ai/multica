use serde::{Deserialize, Serialize};

fn normalize_server_url(raw: &str) -> String {
    let trimmed = raw.trim().trim_end_matches('/');
    if trimmed.is_empty() {
        return "http://localhost:8080".to_string();
    }

    let lower = trimmed.to_ascii_lowercase();
    if matches!(
        lower.as_str(),
        "multica.ai"
            | "https://multica.ai"
            | "http://multica.ai"
            | "https://www.multica.ai"
            | "http://www.multica.ai"
    ) {
        return "https://api.multica.ai".to_string();
    }

    trimmed.to_string()
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WatchedWorkspace {
    pub id: String,
    pub name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AppConfig {
    pub server_url: String,
    pub app_url: Option<String>,
    pub token: Option<String>,
    pub workspace_id: Option<String>,
    pub watched_workspaces: Option<Vec<WatchedWorkspace>>,
}

impl AppConfig {
    pub fn load() -> Result<Self, String> {
        let home = dirs::home_dir().ok_or("Cannot find home directory")?;
        let config_path = home.join(".multica").join("config.json");
        if !config_path.exists() {
            // Return default config if it doesn't exist
            return Ok(Self {
                server_url: "http://localhost:8080".to_string(),
                app_url: None,
                token: None,
                workspace_id: None,
                watched_workspaces: None,
            });
        }
        let data = std::fs::read_to_string(&config_path)
            .map_err(|e| format!("Failed to read config: {}", e))?;
        let mut config: Self =
            serde_json::from_str(&data).map_err(|e| format!("Failed to parse config: {}", e))?;
        config.server_url = normalize_server_url(&config.server_url);
        Ok(config)
    }

    pub fn save(&self) -> Result<(), String> {
        let home = dirs::home_dir().ok_or("Cannot find home directory")?;
        let multica_dir = home.join(".multica");
        if !multica_dir.exists() {
            std::fs::create_dir_all(&multica_dir)
                .map_err(|e| format!("Failed to create .multica directory: {}", e))?;
        }
        let config_path = multica_dir.join("config.json");
        let mut normalized = self.clone();
        normalized.server_url = normalize_server_url(&normalized.server_url);
        let data = serde_json::to_string_pretty(&normalized)
            .map_err(|e| format!("Failed to serialize config: {}", e))?;
        std::fs::write(&config_path, data)
            .map_err(|e| format!("Failed to write config: {}", e))?;
        Ok(())
    }

    pub fn multica_exe_path() -> Result<String, String> {
        // Try PATH first
        if let Ok(output) = std::process::Command::new("where")
            .arg("multica.exe")
            .output()
        {
            if output.status.success() {
                let path = String::from_utf8_lossy(&output.stdout);
                let line = path.lines().next().unwrap_or("").trim();
                if !line.is_empty() {
                    return Ok(line.to_string());
                }
            }
        }
        // Check known location
        let candidates = vec![
            "C:\\Users\\Administrator\\Desktop\\AICODING\\multica\\multica.exe",
        ];
        for path in candidates {
            if std::path::Path::new(path).exists() {
                return Ok(path.to_string());
            }
        }
        Err("multica.exe not found".to_string())
    }
}
