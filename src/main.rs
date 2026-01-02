use std::{
    env,
    fs::File,
    io::{BufRead, BufReader},
    path::PathBuf,
    time::Duration,
};

use anyhow::{anyhow, Context, Result};
use chrono::{DateTime, Local};
use reqwest::StatusCode;
use serde::{Deserialize, Serialize};

const URL: &str = "https://air.cnemc.cn:18007/CityData/GetAQIDataPublishLive?cityName=%E5%B9%BF%E5%B7%9E%E5%B8%82";

// Ignore-alert station names (exact match)
const IGNORE_POSITION_NAMES: [&str; 2] = ["帽峰山", "帽峰山森林公园"];

#[derive(Debug, Clone, Deserialize)]
struct AQIData {
    #[serde(rename = "PositionName")]
    position_name: Option<String>,

    #[serde(rename = "Quality")]
    quality: Option<String>,

    #[serde(rename = "AQI")]
    aqi: Option<String>,

    #[serde(rename = "O3")]
    o3: Option<String>,

    #[serde(rename = "NO2")]
    no2: Option<String>,

    #[serde(rename = "PM10")]
    pm10: Option<String>,

    #[serde(rename = "PM2_5")]
    pm25: Option<String>,

    #[serde(rename = "SO2")]
    so2: Option<String>,

    #[serde(rename = "CO")]
    co: Option<String>,

    #[serde(rename = "Latitude")]
    latitude: Option<String>,

    #[serde(rename = "Longitude")]
    longitude: Option<String>,

    #[serde(rename = "TimePoint")]
    time_point: Option<String>,
}

#[derive(Debug, Clone, Default)]
struct Config {
    webhook_key: String,
    dingtalk_access_token: String,
}

#[derive(Debug, Serialize)]
struct WechatWorkWebhook {
    #[serde(rename = "msgtype")]
    msg_type: String,

    #[serde(skip_serializing_if = "Option::is_none")]
    markdown: Option<WechatMarkdownContent>,
}

#[derive(Debug, Serialize)]
struct WechatMarkdownContent {
    content: String,
}

#[derive(Debug, Serialize)]
struct DingTalkWebhook {
    #[serde(rename = "msgtype")]
    msg_type: String,
    markdown: DingTalkMarkdown,
    #[serde(skip_serializing_if = "Option::is_none")]
    at: Option<DingTalkAt>,
}

#[derive(Debug, Serialize)]
struct DingTalkMarkdown {
    title: String,
    text: String,
}

#[derive(Debug, Serialize)]
struct DingTalkAt {
    #[serde(rename = "atMobiles", skip_serializing_if = "Vec::is_empty", default)]
    at_mobiles: Vec<String>,
    #[serde(rename = "atUserIds", skip_serializing_if = "Vec::is_empty", default)]
    at_user_ids: Vec<String>,
    #[serde(rename = "isAtAll")]
    is_at_all: bool,
}

fn exe_dir() -> Result<PathBuf> {
    let exe = env::current_exe().context("Failed to get executable path")?;
    Ok(exe
        .parent()
        .ok_or_else(|| anyhow!("Failed to determine executable directory"))?
        .to_path_buf())
}

fn read_config_from_env(env_path: PathBuf) -> Result<Config> {
    let file = File::open(&env_path)
        .with_context(|| format!(".env file not found at: {}", env_path.display()))?;
    let reader = BufReader::new(file);

    let mut config = Config::default();
    for line in reader.lines() {
        let line = line.context("error reading .env file")?;
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }

        if let Some(value) = line.strip_prefix("WEBHOOK_KEY=") {
            config.webhook_key = value.trim().to_string();
        } else if let Some(value) = line.strip_prefix("DINGTALK_ACCESS_TOKEN=") {
            config.dingtalk_access_token = value.trim().to_string();
        }
    }

    Ok(config)
}

fn get_config() -> Result<Config> {
    let env_path = exe_dir()?.join(".env");

    match read_config_from_env(env_path) {
        Ok(cfg) => {
            if cfg.webhook_key.is_empty() && cfg.dingtalk_access_token.is_empty() {
                return Err(anyhow!(
                    "No webhook configuration found in .env file (WEBHOOK_KEY / DINGTALK_ACCESS_TOKEN)"
                ));
            }
            Ok(cfg)
        }
        Err(err) => {
            eprintln!("Warning: {err}");
            let cfg = Config {
                webhook_key: env::var("WEBHOOK_KEY").unwrap_or_default(),
                dingtalk_access_token: env::var("DINGTALK_ACCESS_TOKEN").unwrap_or_default(),
            };
            if cfg.webhook_key.is_empty() && cfg.dingtalk_access_token.is_empty() {
                return Err(anyhow!(
                    "No webhook configuration found in .env file or environment variables"
                ));
            }
            Ok(cfg)
        }
    }
}

async fn fetch_aqi_data(client: &reqwest::Client) -> Result<Vec<AQIData>> {
    let resp = client
        .get(URL)
        .timeout(Duration::from_secs(30))
        .send()
        .await
        .context("Failed to fetch data")?;

    let status = resp.status();
    if status != StatusCode::OK {
        return Err(anyhow!("AQI request failed with status: {status}"));
    }

    let data = resp.json::<Vec<AQIData>>().await.context("Failed to decode JSON")?;
    Ok(data)
}

fn is_missing(value: &Option<String>) -> bool {
    match value.as_deref() {
        None => true,
        Some(v) => {
            let v = v.trim();
            v.is_empty() || v == "—"
        }
    }
}

fn has_missing_data(station: &AQIData) -> bool {
    is_missing(&station.aqi)
        || is_missing(&station.pm25)
        || is_missing(&station.pm10)
        || is_missing(&station.o3)
        || is_missing(&station.no2)
        || is_missing(&station.so2)
        || is_missing(&station.co)
}

fn is_ignored_station(station: &AQIData) -> bool {
    let Some(name) = station.position_name.as_deref() else {
        return false;
    };
    let name = name.trim();
    IGNORE_POSITION_NAMES.iter().any(|n| n == &name)
}

fn get_missing_factors(station: &AQIData) -> Vec<&'static str> {
    let mut missing = Vec::new();
    if is_missing(&station.aqi) {
        missing.push("AQI");
    }
    if is_missing(&station.pm25) {
        missing.push("PM2.5");
    }
    if is_missing(&station.pm10) {
        missing.push("PM10");
    }
    if is_missing(&station.o3) {
        missing.push("O3");
    }
    if is_missing(&station.no2) {
        missing.push("NO2");
    }
    if is_missing(&station.so2) {
        missing.push("SO2");
    }
    if is_missing(&station.co) {
        missing.push("CO");
    }
    missing
}

fn format_missing_factors(factors: &[&'static str]) -> String {
    if factors.is_empty() {
        return "无".to_string();
    }
    factors.join("、")
}

fn format_time(problem_stations: &[AQIData]) -> String {
    let Some(tp) = problem_stations
        .first()
        .and_then(|s| s.time_point.as_deref())
        .map(str::trim)
        .filter(|s| !s.is_empty())
    else {
        return "Unknown".to_string();
    };

    match DateTime::parse_from_rfc3339(tp) {
        Ok(dt) => dt.with_timezone(&Local).format("%Y-%m-%d %H:%M:%S").to_string(),
        Err(_) => tp.to_string(),
    }
}

async fn send_alert_to_wechat_work(
    client: &reqwest::Client,
    problem_stations: &[AQIData],
    webhook_key: &str,
) -> Result<()> {
    if problem_stations.is_empty() {
        return Ok(());
    }
    if webhook_key.trim().is_empty() {
        return Ok(());
    }

    let formatted_time = format_time(problem_stations);
    let mut markdown = format!(
        "## 🚨 广州市空气质量监测站点数据异常警报({})\n",
        formatted_time
    );
    markdown.push_str("以下站点存在数据缺失问题，请及时关注：\n\n");

    for station in problem_stations {
        let name = station
            .position_name
            .as_deref()
            .unwrap_or("Unknown")
            .trim();
        let missing = get_missing_factors(station);
        markdown.push_str(&format!(
            "**{}**\n<font color=\"warning\">缺失因子: {}</font>\n\n",
            name,
            format_missing_factors(&missing)
        ));
    }
    markdown.push_str("> 请相关技术人员尽快检查设备状态和数据传输链路。（缺失数据基于总站发布平台）");

    let webhook_url = format!(
        "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key={}",
        webhook_key.trim()
    );
    let payload = WechatWorkWebhook {
        msg_type: "markdown".to_string(),
        markdown: Some(WechatMarkdownContent { content: markdown }),
    };

    let resp = client
        .post(webhook_url)
        .json(&payload)
        .timeout(Duration::from_secs(30))
        .send()
        .await
        .context("Failed to send WeChat Work webhook")?;

    if resp.status() != StatusCode::OK {
        return Err(anyhow!(
            "wechat work webhook request failed with status: {}",
            resp.status()
        ));
    }

    Ok(())
}

async fn send_alert_to_dingtalk(
    client: &reqwest::Client,
    problem_stations: &[AQIData],
    access_token: &str,
) -> Result<()> {
    if problem_stations.is_empty() {
        return Ok(());
    }
    if access_token.trim().is_empty() {
        return Ok(());
    }

    let formatted_time = format_time(problem_stations);
    let title = format!("广州市空气质量监测站点数据异常警报({})", formatted_time);

    let mut text = "### 🚨 广州市空气质量监测站点数据异常警报\n".to_string();
    text.push_str(&format!("#### {}\n", formatted_time));
    text.push_str("以下站点存在数据缺失问题，请及时关注：\n\n");

    for station in problem_stations {
        let name = station
            .position_name
            .as_deref()
            .unwrap_or("Unknown")
            .trim();
        let missing = get_missing_factors(station);
        text.push_str(&format!(
            "- **{}**\n  - 缺失因子: {}\n\n",
            name,
            format_missing_factors(&missing)
        ));
    }
    text.push_str("> 请相关技术人员尽快检查设备状态和数据传输链路。（缺失数据基于总站发布平台）");

    let webhook_url = format!(
        "https://oapi.dingtalk.com/robot/send?access_token={}",
        access_token.trim()
    );
    let payload = DingTalkWebhook {
        msg_type: "markdown".to_string(),
        markdown: DingTalkMarkdown { title, text },
        at: Some(DingTalkAt {
            at_mobiles: Vec::new(),
            at_user_ids: Vec::new(),
            is_at_all: false,
        }),
    };

    let resp = client
        .post(webhook_url)
        .json(&payload)
        .timeout(Duration::from_secs(30))
        .send()
        .await
        .context("Failed to send DingTalk webhook")?;

    if resp.status() != StatusCode::OK {
        return Err(anyhow!(
            "dingtalk webhook request failed with status: {}",
            resp.status()
        ));
    }

    Ok(())
}

#[tokio::main]
async fn main() -> Result<()> {
    let config = get_config()?;
    let client = reqwest::Client::new();

    let data = fetch_aqi_data(&client).await?;
    let problem_stations: Vec<AQIData> = data
        .into_iter()
        .filter(|s| !is_ignored_station(s))
        .filter(|s| has_missing_data(s))
        .collect();

    if problem_stations.is_empty() {
        println!("所有（非忽略名单）站点数据正常");
        return Ok(());
    }

    if !config.webhook_key.is_empty() {
        match send_alert_to_wechat_work(&client, &problem_stations, &config.webhook_key).await {
            Ok(()) => println!("已成功发送警报到企业微信"),
            Err(err) => eprintln!("Failed to send alert to WeChat Work: {err}"),
        }
    }

    if !config.dingtalk_access_token.is_empty() {
        match send_alert_to_dingtalk(&client, &problem_stations, &config.dingtalk_access_token).await {
            Ok(()) => println!("已成功发送警报到钉钉"),
            Err(err) => eprintln!("Failed to send alert to DingTalk: {err}"),
        }
    }

    println!("发现 {} 个异常站点（已排除忽略名单）", problem_stations.len());
    Ok(())
}
