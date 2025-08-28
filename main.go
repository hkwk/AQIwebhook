package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AQIData struct {
	PositionName string `json:"PositionName"`
	Quality      string `json:"Quality"`
	AQI          string `json:"AQI"`
	O3           string `json:"O3"`
	NO2          string `json:"NO2"`
	PM10         string `json:"PM10"`
	PM25         string `json:"PM2_5"`
	SO2          string `json:"SO2"`
	CO           string `json:"CO"`
	Latitude     string `json:"Latitude"`
	Longitude    string `json:"Longitude"`
	TimePoint    string `json:"TimePoint"`
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

const url = "https://air.cnemc.cn:18007/CityData/GetAQIDataPublishLive?cityName=%E5%B9%BF%E5%B7%9E%E5%B8%82"

// 配置结构体
type Config struct {
	WebhookKey          string
	DingTalkAccessToken string
}

// 从.env文件获取配置
func getConfig() Config {
	// 获取可执行文件所在目录
	exe, err := os.Executable()
	if err != nil {
		log.Fatal("Failed to get executable path:", err)
	}
	exeDir := filepath.Dir(exe)

	// 尝试从.env文件读取
	envPath := filepath.Join(exeDir, ".env")
	config, err := readConfigFromEnv(envPath)
	if err != nil {
		log.Printf("Warning: %v", err)

		// 如果.env文件不存在，尝试从环境变量读取
		config.WebhookKey = os.Getenv("WEBHOOK_KEY")
		config.DingTalkAccessToken = os.Getenv("DINGTALK_ACCESS_TOKEN")

		if config.WebhookKey == "" && config.DingTalkAccessToken == "" {
			log.Fatal("No webhook configuration found in .env file or environment variables")
		}
	}

	return config
}

// 从.env文件读取配置
func readConfigFromEnv(envPath string) (Config, error) {
	config := Config{}

	file, err := os.Open(envPath)
	if err != nil {
		return config, fmt.Errorf(".env file not found")
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "WEBHOOK_KEY=") {
			config.WebhookKey = strings.TrimPrefix(line, "WEBHOOK_KEY=")
		} else if strings.HasPrefix(line, "DINGTALK_ACCESS_TOKEN=") {
			config.DingTalkAccessToken = strings.TrimPrefix(line, "DINGTALK_ACCESS_TOKEN=")
		}
	}

	if err := scanner.Err(); err != nil {
		return config, fmt.Errorf("error reading .env file: %v", err)
	}

	return config, nil
}

func fetchAQIData() ([]AQIData, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data []AQIData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

// 检查站点是否有数据缺失
func hasMissingData(station AQIData) bool {
	return station.AQI == "—" ||
		station.PM25 == "—" ||
		station.PM10 == "—" ||
		station.O3 == "—" ||
		station.NO2 == "—" ||
		station.SO2 == "—" ||
		station.CO == "—"
}

// 获取缺失数据的因子列表
func getMissingFactors(station AQIData) []string {
	var missingFactors []string

	if station.AQI == "—" {
		missingFactors = append(missingFactors, "AQI")
	}
	if station.PM25 == "—" {
		missingFactors = append(missingFactors, "PM2.5")
	}
	if station.PM10 == "—" {
		missingFactors = append(missingFactors, "PM10")
	}
	if station.O3 == "—" {
		missingFactors = append(missingFactors, "O3")
	}
	if station.NO2 == "—" {
		missingFactors = append(missingFactors, "NO2")
	}
	if station.SO2 == "—" {
		missingFactors = append(missingFactors, "SO2")
	}
	if station.CO == "—" {
		missingFactors = append(missingFactors, "CO")
	}

	return missingFactors
}

// 发送警报到企业微信
func sendAlertToWechatWork(problemStations []AQIData, webhookKey string) error {
	if len(problemStations) == 0 {
		return nil
	}

	// 格式化时间
	formattedTime := formatTime(problemStations)

	// 构建Markdown警报内容
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

	markdownContent += "> 请相关技术人员尽快检查设备状态和数据传输链路。（缺失数据基于总站发布平台）"

	// 构建Webhook请求
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

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wechat work webhook request failed with status: %s", resp.Status)
	}

	return nil
}

// 发送警报到钉钉
func sendAlertToDingTalk(problemStations []AQIData, accessToken string) error {
	if len(problemStations) == 0 {
		return nil
	}

	// 格式化时间
	formattedTime := formatTime(problemStations)

	// 构建钉钉Markdown内容
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

	text += "> 请相关技术人员尽快检查设备状态和数据传输链路。（缺失数据基于总站发布平台）"

	// 构建Webhook请求
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

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dingtalk webhook request failed with status: %s", resp.Status)
	}

	return nil
}

// 格式化时间
func formatTime(problemStations []AQIData) string {
	if len(problemStations) == 0 {
		return "Unknown"
	}

	var formattedTime string
	t, err := time.Parse(time.RFC3339, problemStations[0].TimePoint)
	if err != nil {
		formattedTime = problemStations[0].TimePoint
	} else {
		formattedTime = t.Format("2006-01-02 15:04:05")
	}

	return formattedTime
}

// 格式化缺失因子列表
func formatMissingFactors(factors []string) string {
	if len(factors) == 0 {
		return "无"
	}

	result := ""
	for i, factor := range factors {
		if i > 0 {
			result += "、"
		}
		result += factor
	}
	return result
}

func main() {
	config := getConfig()

	data, err := fetchAQIData()
	if err != nil {
		log.Fatal("Failed to fetch data:", err)
	}

	// 筛选出有数据缺失的站点
	var problemStations []AQIData
	for _, station := range data {
		if hasMissingData(station) {
			problemStations = append(problemStations, station)
		}
	}

	if len(problemStations) == 0 {
		fmt.Println("所有站点数据正常")
		return
	}

	// 发送到企业微信（如果配置了）
	if config.WebhookKey != "" {
		if err := sendAlertToWechatWork(problemStations, config.WebhookKey); err != nil {
			log.Printf("Failed to send alert to WeChat Work: %v", err)
		} else {
			fmt.Println("已成功发送警报到企业微信")
		}
	}

	// 发送到钉钉（如果配置了）
	if config.DingTalkAccessToken != "" {
		if err := sendAlertToDingTalk(problemStations, config.DingTalkAccessToken); err != nil {
			log.Printf("Failed to send alert to DingTalk: %v", err)
		} else {
			fmt.Println("已成功发送警报到钉钉")
		}
	}

	fmt.Printf("发现 %d 个异常站点\n", len(problemStations))
}
