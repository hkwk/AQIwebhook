package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// -------------------- 数据结构 --------------------
type AQIData struct {
	PositionName     string `json:"PositionName"`
	Quality          string `json:"Quality"`
	AQI              string `json:"AQI"`
	O3               string `json:"O3"`
	NO2              string `json:"NO2"`
	PM10             string `json:"PM10"`
	PM25             string `json:"PM2_5"` // 与接口字段一致
	SO2              string `json:"SO2"`
	CO               string `json:"CO"`
	Latitude         string `json:"Latitude"`
	Longitude        string `json:"Longitude"`
	TimePoint        string `json:"TimePoint"`
	StationCode      string `json:"StationCode,omitempty"`
	PrimaryPollutant string `json:"PrimaryPollutant,omitempty"`
}

// 企业微信Webhook请求结构
type WechatWorkWebhook struct {
	MsgType  string          `json:"msgtype"`
	Text     TextContent     `json:"text,omitempty"`
	Markdown MarkdownContent `json:"markdown,omitempty"`
}
type TextContent struct {
	Content string `json:"content"`
}
type MarkdownContent struct {
	Content string `json:"content"`
}

// 钉钉Webhook请求结构
type DingTalkWebhook struct {
	MsgType  string           `json:"msgtype"`
	Markdown DingTalkMarkdown `json:"markdown"`
	At       DingTalkAt       `json:"at,omitempty"`
}
type DingTalkMarkdown struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}
type DingTalkAt struct {
	AtMobiles []string `json:"atMobiles,omitempty"`
	AtUserIds []string `json:"atUserIds,omitempty"`
	IsAtAll   bool     `json:"isAtAll,omitempty"`
}

// -------------------- 常量 & 配置 --------------------
const url = "https://air.cnemc.cn:18007/CityData/GetAQIDataPublishLive?cityName=%E5%B9%BF%E5%B7%9E%E5%B8%82"

// 要忽略告警的站点名称（完全匹配）
var ignorePositionNames = map[string]struct{}{
	"帽峰山森林公园": {},
}

// 配置结构体
type Config struct {
	WechatWebhookKey     string
	DingTalkAccessToken  string
	HTTPClientTimeoutSec int
}

// -------------------- 配置读取 --------------------

// 从.env文件读取配置（简单实现）
func readConfigFromEnv(envPath string) (Config, error) {
	conf := Config{}
	f, err := os.Open(envPath)
	if err != nil {
		return conf, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch k {
		case "WEBHOOK_KEY":
			conf.WechatWebhookKey = v
		case "DINGTALK_ACCESS_TOKEN":
			conf.DingTalkAccessToken = v
		case "HTTP_TIMEOUT_SEC":
			// 这里不做直接解析，main 会覆盖默认值
			_ = v
		}
	}
	if err := scanner.Err(); err != nil {
		return conf, err
	}
	return conf, nil
}

// 获取配置：优先环境变量，其次 exe 目录 .env，再次当前工作目录 .env
func getConfig() Config {
	cfg := Config{
		HTTPClientTimeoutSec: 10,
	}
	// 环境变量优先
	cfg.WechatWebhookKey = strings.TrimSpace(os.Getenv("WEBHOOK_KEY"))
	cfg.DingTalkAccessToken = strings.TrimSpace(os.Getenv("DINGTALK_ACCESS_TOKEN"))

	// 尝试 exe 目录 .env
	if cfg.WechatWebhookKey == "" || cfg.DingTalkAccessToken == "" {
		if exe, err := os.Executable(); err == nil {
			envPath := filepath.Join(filepath.Dir(exe), ".env")
			if _, err := os.Stat(envPath); err == nil {
				if conf, err := readConfigFromEnv(envPath); err == nil {
					if cfg.WechatWebhookKey == "" {
						cfg.WechatWebhookKey = strings.TrimSpace(conf.WechatWebhookKey)
					}
					if cfg.DingTalkAccessToken == "" {
						cfg.DingTalkAccessToken = strings.TrimSpace(conf.DingTalkAccessToken)
					}
				}
			}
		}
	}

	// 尝试当前工作目录 .env
	if cfg.WechatWebhookKey == "" || cfg.DingTalkAccessToken == "" {
		if cwd, err := os.Getwd(); err == nil {
			envPath := filepath.Join(cwd, ".env")
			if _, err := os.Stat(envPath); err == nil {
				if conf, err := readConfigFromEnv(envPath); err == nil {
					if cfg.WechatWebhookKey == "" {
						cfg.WechatWebhookKey = strings.TrimSpace(conf.WechatWebhookKey)
					}
					if cfg.DingTalkAccessToken == "" {
						cfg.DingTalkAccessToken = strings.TrimSpace(conf.DingTalkAccessToken)
					}
				}
			}
		}
	}

	// 可选环境变量覆盖 timeout
	if t := os.Getenv("HTTP_TIMEOUT_SEC"); t != "" {
		if v, err := strconvAtoiSafe(t); err == nil && v > 0 {
			cfg.HTTPClientTimeoutSec = v
		}
	}

	return cfg
}

// small helper to parse int safely
func strconvAtoiSafe(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// -------------------- HTTP 客户端 --------------------
func makeHTTPClient(timeoutSec int) *http.Client {
	return &http.Client{
		Timeout: time.Duration(timeoutSec) * time.Second,
	}
}

// -------------------- 数据获取与解析 --------------------

// fetchAQIData: 尝试理解多种返回形态，优先直接解为数组
func fetchAQIData(ctx context.Context, client *http.Client) ([]AQIData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("api returned http status %d: %s", resp.StatusCode, string(body))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var arr []AQIData
	// 1) 直接数组
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}

	// 2) 包裹对象, 找常见字段名
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		keys := []string{"Data", "data", "Rows", "rows", "Table", "table", "records", "Records"}
		for _, k := range keys {
			if v, ok := obj[k]; ok {
				if err := json.Unmarshal(v, &arr); err == nil {
					return arr, nil
				}
			}
		}
		// 尝试第一个数组字段
		for _, v := range obj {
			if json.Unmarshal(v, &arr) == nil {
				return arr, nil
			}
		}
	}

	return nil, errors.New("cannot decode response as expected JSON array")
}

// -------------------- 缺失检测 & 时间解析 --------------------

// isMissingValue: 识别常见的缺失占位
func isMissingValue(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	l := strings.ToLower(s)
	if l == "-" || l == "—" || l == "na" || l == "n/a" || l == "null" {
		return true
	}
	// 处理中文破折号等
	if strings.Contains(s, "—") || strings.Contains(s, "缺失") {
		return true
	}
	return false
}

func hasMissingData(st AQIData) bool {
	return isMissingValue(st.AQI) ||
		isMissingValue(st.PM25) ||
		isMissingValue(st.PM10) ||
		isMissingValue(st.O3) ||
		isMissingValue(st.NO2) ||
		isMissingValue(st.SO2) ||
		isMissingValue(st.CO)
}

func getMissingFactors(st AQIData) []string {
	var missing []string
	if isMissingValue(st.AQI) {
		missing = append(missing, "AQI")
	}
	if isMissingValue(st.PM25) {
		missing = append(missing, "PM2.5")
	}
	if isMissingValue(st.PM10) {
		missing = append(missing, "PM10")
	}
	if isMissingValue(st.O3) {
		missing = append(missing, "O3")
	}
	if isMissingValue(st.NO2) {
		missing = append(missing, "NO2")
	}
	if isMissingValue(st.SO2) {
		missing = append(missing, "SO2")
	}
	if isMissingValue(st.CO) {
		missing = append(missing, "CO")
	}
	return missing
}

// 解析多种时间格式（你的样本是 "2006-01-02T15:04:05" 无时区）
func parseTimeFlexible(ts string) (time.Time, error) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Time{}, errors.New("empty time")
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05", // 样本形式（无时区）
		"2006-01-02 15:04:05",
		"2006/01/02 15:04:05",
		"2006-01-02",
	}
	var lastErr error
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, ts, time.Local); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

func formatTimeForAlert(ps []AQIData) string {
	if len(ps) == 0 {
		return "Unknown"
	}
	if t, err := parseTimeFlexible(ps[0].TimePoint); err == nil {
		return t.Format("2006-01-02 15:04:05")
	}
	return ps[0].TimePoint
}

func formatMissingFactors(factors []string) string {
	if len(factors) == 0 {
		return "无"
	}
	return strings.Join(factors, "、")
}

// -------------------- 告警发送 --------------------

// 发送企业微信
func sendAlertToWechatWork(problemStations []AQIData, webhookKey string, client *http.Client) error {
	if len(problemStations) == 0 || webhookKey == "" {
		return nil
	}

	formattedTime := formatTimeForAlert(problemStations)

	markdownContent := fmt.Sprintf("## 🚨 广州市空气质量监测站点数据异常警报(%s)\n", formattedTime)
	markdownContent += "以下站点存在数据缺失问题，请及时关注：\n\n"

	for _, station := range problemStations {
		missingFactors := getMissingFactors(station)
		markdownContent += fmt.Sprintf(
			"**%s**\n<font color=\"warning\">缺失因子: %s</font>\n\n",
			station.PositionName,
			formatMissingFactors(missingFactors),
		)
	}

	markdownContent += "> 请相关技术人员尽快检查设备状态和数据传输链路。"

	webhookURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", webhookKey)
	webhookData := WechatWorkWebhook{
		MsgType: "markdown",
		Markdown: MarkdownContent{
			Content: markdownContent,
		},
	}

	jsonData, err := json.Marshal(webhookData)
	if err != nil {
		return err
	}

	req, _ := http.NewRequest(http.MethodPost, webhookURL, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wechat webhook http status %d: %s", resp.StatusCode, string(body))
	}

	// 尝试解析 errcode（企业微信）
	var respObj map[string]interface{}
	if err := json.Unmarshal(body, &respObj); err == nil {
		if ec, ok := respObj["errcode"]; ok {
			if fv, ok := ec.(float64); ok && fv != 0 {
				return fmt.Errorf("wechat webhook errcode=%v, body=%s", ec, string(body))
			}
		}
	}

	return nil
}

// 发送钉钉
func sendAlertToDingTalk(problemStations []AQIData, accessToken string, client *http.Client) error {
	if len(problemStations) == 0 || accessToken == "" {
		return nil
	}

	formattedTime := formatTimeForAlert(problemStations)
	title := fmt.Sprintf("广州市空气质量监测站点数据异常警报(%s)", formattedTime)

	text := "### 🚨 广州市空气质量监测站点数据异常警报\n"
	text += "#### " + formattedTime + "\n"
	text += "以下站点存在数据缺失问题，请及时关注：\n\n"

	for _, station := range problemStations {
		missingFactors := getMissingFactors(station)
		text += fmt.Sprintf(
			"- **%s**\n  - 缺失因子: %s\n\n",
			station.PositionName,
			formatMissingFactors(missingFactors),
		)
	}

	text += "> 请相关技术人员尽快检查设备状态和数据传输链路。"

	webhookURL := fmt.Sprintf("https://oapi.dingtalk.com/robot/send?access_token=%s", accessToken)
	webhookData := DingTalkWebhook{
		MsgType: "markdown",
		Markdown: DingTalkMarkdown{
			Title: title,
			Text:  text,
		},
		At: DingTalkAt{
			IsAtAll: false,
		},
	}

	jsonData, err := json.Marshal(webhookData)
	if err != nil {
		return err
	}

	req, _ := http.NewRequest(http.MethodPost, webhookURL, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dingtalk webhook http status %d: %s", resp.StatusCode, string(body))
	}

	// 钉钉也返回 errcode 字段
	var respObj map[string]interface{}
	if err := json.Unmarshal(body, &respObj); err == nil {
		if ec, ok := respObj["errcode"]; ok {
			if fv, ok := ec.(float64); ok && fv != 0 {
				return fmt.Errorf("dingtalk webhook errcode=%v, body=%s", ec, string(body))
			}
		}
	}

	return nil
}

// -------------------- main --------------------

func main() {
	cfg := getConfig()

	if cfg.WechatWebhookKey == "" && cfg.DingTalkAccessToken == "" {
		log.Println("警告: 未配置任何 webhook（环境变量 WEBHOOK_KEY / DINGTALK_ACCESS_TOKEN 或 .env），程序将仅进行数据抓取与检测。")
	}

	client := makeHTTPClient(cfg.HTTPClientTimeoutSec)
	ctx := context.Background()

	// 简单重试策略（最多 3 次）
	var data []AQIData
	var err error
	for i := 0; i < 3; i++ {
		data, err = fetchAQIData(ctx, client)
		if err == nil {
			break
		}
		wait := time.Duration(500*(1<<i)) * time.Millisecond
		time.Sleep(wait)
	}
	if err != nil {
		log.Fatalf("Failed to fetch data: %v", err)
	}

	// 筛选出有数据缺失的站点，但 **忽略 ignorePositionNames 列表中的站点**
	var problemStations []AQIData
	for _, station := range data {
		// 如果是忽略名单，跳过
		if _, ok := ignorePositionNames[station.PositionName]; ok {
			continue
		}
		if hasMissingData(station) {
			problemStations = append(problemStations, station)
		}
	}

	if len(problemStations) == 0 {
		fmt.Println("所有（非忽略名单）站点数据正常")
		return
	}

	// 发送到企业微信（如果配置了）
	if cfg.WechatWebhookKey != "" {
		if err := sendAlertToWechatWork(problemStations, cfg.WechatWebhookKey, client); err != nil {
			log.Printf("Failed to send alert to WeChat Work: %v", err)
		} else {
			fmt.Println("已成功发送警报到企业微信")
		}
	}

	// 发送到钉钉（如果配置了）
	if cfg.DingTalkAccessToken != "" {
		if err := sendAlertToDingTalk(problemStations, cfg.DingTalkAccessToken, client); err != nil {
			log.Printf("Failed to send alert to DingTalk: %v", err)
		} else {
			fmt.Println("已成功发送警报到钉钉")
		}
	}

	fmt.Printf("发现 %d 个异常站点（已排除忽略名单）\n", len(problemStations))
}
