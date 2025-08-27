package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

func init() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Warning: .env file not found")
	}
}

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

const url = "https://air.cnemc.cn:18007/CityData/GetAQIDataPublishLive?cityName=%E5%B9%BF%E5%B7%9E%E5%B8%82"

// 从环境变量获取Webhook Key
func getWebhookKey() string {
	key := os.Getenv("WEBHOOK_KEY")
	if key == "" {
		log.Fatal("WEBHOOK_KEY environment variable is not set")
	}
	return key
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

func sendAlertToWechatWork(problemStations []AQIData) error {
	if len(problemStations) == 0 {
		fmt.Println("所有站点数据正常，无需发送警报")
		return nil
	}

	// 格式化时间
	var formattedTime string
	if len(problemStations) > 0 {
		t, err := time.Parse(time.RFC3339, problemStations[0].TimePoint)
		if err != nil {
			formattedTime = problemStations[0].TimePoint
		} else {
			formattedTime = t.Format("2006-01-02 15:04:05")
		}
	}

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

	markdownContent += "> 请相关技术人员尽快检查设备状态和数据传输链路。"

	// 构建Webhook请求
	webhookURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", getWebhookKey())
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
		return fmt.Errorf("webhook request failed with status: %s", resp.Status)
	}

	return nil
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

	if err := sendAlertToWechatWork(problemStations); err != nil {
		log.Fatal("Failed to send alert to WeChat Work:", err)
	}

	if len(problemStations) > 0 {
		fmt.Printf("已发送 %d 个异常站点的警报\n", len(problemStations))
	} else {
		fmt.Println("所有站点数据正常")
	}
}
