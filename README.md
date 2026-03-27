## Introduction

主要用于定时监控中国环境监测总站大气环境监测发布数据，查看数据传输缺失情况。将数据缺失情况发送到企业微信以及钉钉机器人，以通知相关人员。

## How to use:

### Build Linux Binary:

```bash
cargo build --release --target x86_64-unknown-linux-musl
```

### Build Windows Binary:

```bash
cargo build --release
```

## Create Env Variables

```bash
touch .env
echo "WEBHOOK_KEY=xxxx" >> .env # 企业微信机器人webhook key

echo "DINGTALK_ACCESS_TOKEN=yyy" >> .env #钉钉机器人webhook key
```