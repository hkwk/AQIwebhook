## AQIwebbook

主要用于定时监控中国环境监测总站大气环境监测发布数据，查看数据传输缺失情况。将数据缺失情况发送到企业微信以及钉钉机器人，以通知相关人员。

### 使用说明


### 编译程序
```bash
git clone https://github.com/hkwk/AQIwebhook.git
cd AQIwebhook
go build
```

### 创建环境变量
```bash
touch .env
echo "WEBHOOK_KEY=xxxx" >> .env # 企业微信机器人webhook key

echo "DINGTALK_ACCESS_TOKEN=yyy" >> .env #钉钉机器人webhook key

```

### 执行程序
```bash
./AQIwebhook

```

![gplv3](https://gnu.ac.cn/graphics/gplv3-rounded-grey-180x60.jpg)
