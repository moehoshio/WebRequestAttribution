# Nginx Request Attribution

🌐 [English](README.md) | [繁體中文](README.zh-TW.md) | **简体中文** | [日本語](README.ja.md)

一个轻量级的 Nginx 访问日志分析工具，提供统计报表和实时监控功能。

## 截图预览

| Dark Mode | Light Mode |
|:-:|:-:|
| ![Dark Mode](docs/screenshot-dark.png) | ![Light Mode](docs/screenshot-light.png) |

## 特点

- 🚀 **单一二进制文件** - Go 编译为单一可执行文件，无需额外 runtime
- 📊 **内置 Web GUI** - 统计报表直接嵌入二进制文件中，无需额外前端部署
- 🔍 **多维度筛选** - 支持按 IP、路径、域名、查询参数、OS、浏览器、状态码等筛选
- 🔑 **关键词追踪** - 自动追踪配置的关键词出现次数
- 📡 **实时监控** - 自动监控日志文件新增内容
- 🐳 **一键部署** - 支持 Docker / Docker Compose 部署
- 💾 **SQLite 存储** - 轻量级数据库，无需外部数据库服务
- 🌐 **多语言界面** - Web GUI 支持简体中文、繁体中文、英文、日文

## 快速开始

### 方式一：直接执行

```bash
# 编译
go build -o nginx-req-attr ./cmd/

# 导入既有日志
./nginx-req-attr -import /var/log/nginx/access.log

# 启动服务（监控日志 + Web GUI）
./nginx-req-attr -config config.json
```

### 方式二：Docker 部署

```bash
# 一键启动
docker-compose up -d

# 或手动 Docker
docker build -t nginx-req-attr .
docker run -d \
  -p 8080:8080 \
  -v /var/log/nginx:/var/log/nginx:ro \
  -v ./data:/app/data \
  nginx-req-attr
```

## 配置

创建 `config.json`：

```json
{
  "log_path": "/var/log/nginx/access.log",
  "log_format": "combined",
  "listen_addr": ":8080",
  "db_path": "./data/stats.db",
  "watch": true,
  "keywords": ["login", "admin", "api", "search"],
  "input_mode": "file",
  "syslog_addr": ":1514",
  "syslog_proto": "udp"
}
```

| 字段 | 说明 | 默认值 |
|---|---|---|
| `log_path` | Nginx 访问日志路径 | `/var/log/nginx/access.log` |
| `log_format` | 日志格式 (combined/vhost_combined) | `combined` |
| `listen_addr` | HTTP 服务监听地址 | `:8080` |
| `db_path` | SQLite 数据库文件路径 | `./data/stats.db` |
| `watch` | 是否实时监控日志 | `true` |
| `keywords` | 要追踪的关键词列表 | `[]` |
| `input_mode` | 输入模式 (`file`/`syslog`/`both`) | `file` |
| `syslog_addr` | Syslog 监听地址 | `:1514` |
| `syslog_proto` | Syslog 协议 (`udp`/`tcp`/`both`) | `udp` |

### 输入模式

- **`file`** — 使用 fsnotify 事件驱动监控日志文件（默认，高效率，无需修改 nginx 配置）
- **`syslog`** — 启动 syslog 接收器，通过网络接收 nginx 日志（适合多实例汇聚）
- **`both`** — 同时使用文件监控和 syslog 接收

#### Syslog 模式配置示例

Nginx 配置加入：
```nginx
access_log syslog:server=127.0.0.1:1514,facility=local7,tag=nginx combined;
```

## API 接口

### GET /api/stats

获取统计摘要，支持以下查询参数筛选：

| 参数 | 说明 |
|---|---|
| `start` | 开始日期 (YYYY-MM-DD) |
| `end` | 结束日期 (YYYY-MM-DD) |
| `ip` | IP 地址 (模糊搜索) |
| `path` | 路径 (模糊搜索) |
| `domain` | 域名 (模糊搜索) |
| `query` | 查询字符串 (模糊搜索) |
| `method` | HTTP 方法 |
| `status` | HTTP 状态码 |
| `os` | 操作系统 |
| `browser` | 浏览器 |
| `keyword` | 关键词 |

**响应示例：**
```json
{
  "total_requests": 12345,
  "top_paths": [{"name": "/api/users", "count": 500}],
  "top_ips": [{"name": "192.168.1.1", "count": 300}],
  "top_domains": [{"name": "example.com", "count": 200}],
  "top_os": [{"name": "Windows", "count": 5000}],
  "top_browsers": [{"name": "Chrome", "count": 8000}],
  "top_keywords": [{"name": "api", "count": 1500}],
  "status_codes": [{"name": "200", "count": 10000}],
  "requests_per_day": [{"date": "2023-10-10", "count": 500}]
}
```

### GET /api/requests

获取请求列表（分页），额外支持：

| 参数 | 说明 |
|---|---|
| `limit` | 每页条数 (默认 100) |
| `offset` | 偏移量 |

## 支持的日志格式

### Combined (默认)
```
$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
```

### Virtual Host Combined
```
$host $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
```

## 开发

```bash
# 运行测试
go test ./...

# 编译
go build -o nginx-req-attr ./cmd/
```

## 许可证

MIT License
