# Web Request Attribution

🌐 [English](README.md) | **繁體中文** | [简体中文](README.zh-CN.md) | [日本語](README.ja.md)

看看是誰在造訪你的網站——資料完全留在自己手上，不經過任何第三方。

Web Request Attribution 會讀取你的 Web 伺服器（Nginx / Apache）存取日誌，
變成一目了然的儀表板：訪問趨勢、熱門頁面、瀏覽器、狀態碼、請求來源世界
地圖等等。它只是一個小小的程式，所有功能都內建——不需要資料庫伺服器、
不需要另外架前端，也不用再安裝任何東西。

## 截圖預覽

| 深色模式 | 淺色模式 |
|:-:|:-:|
| ![Dark Mode](docs/screenshot-dark.png) | ![Light Mode](docs/screenshot-light.png) |

## 特色

- 🚀 **單一檔案、零依賴** — 下載一個程式就能直接執行
- 📊 **內建儀表板** — 打開瀏覽器，統計報表就在眼前
- 🗺️ **世界地圖** — 看請求來自哪些國家/地區（免費、無需 API 金鑰）；滑鼠懸停氣泡即可查看詳情
- 📡 **即時更新** — 新的日誌內容自動出現在儀表板上
- 🔍 **強大篩選** — 依 IP、頁面、域名、瀏覽器、作業系統、狀態碼、關鍵詞、日期深入分析；條件前加 `!` 即可排除（支援多個排除條件）
- 🔐 **可選登入** — 自己電腦上可開放使用，放上伺服器可強制帳號登入
- 🌐 **4 種語言** — 繁體中文、简体中文、English、日本語
- 🐳 **支援 Docker** — 一行指令完成部署

## 三步驟開始使用

**1. 下載**：從[最新 Release](https://github.com/moehoshio/WebRequestAttribution/releases/latest) 下載符合你系統的檔案：

| 你的系統 | 下載檔案 |
|---|---|
| Linux（x86_64） | `web-req-attr-linux-amd64` |
| Linux（ARM，如樹莓派） | `web-req-attr-linux-arm64` |
| macOS（Intel） | `web-req-attr-darwin-amd64` |
| macOS（Apple Silicon） | `web-req-attr-darwin-arm64` |
| Windows | `web-req-attr-windows-amd64.exe` |

**2. 執行**（Linux/macOS；Windows 直接雙擊 `.exe` 即可）：

```bash
chmod +x web-req-attr-linux-amd64
./web-req-attr-linux-amd64
```

**3. 打開瀏覽器**前往 <http://localhost:8080>。完成！🎉

第一次執行時程式會自動建立 `config.json`。之後要監看哪些日誌檔、追蹤
哪些關鍵詞，都可以直接在瀏覽器的「**設定**」分頁裡完成。

## 指向你的日誌檔

要分析網站流量，只需要告訴它存取日誌在哪裡。可以在瀏覽器的「**設定**」
分頁新增日誌來源，或編輯 `config.json`：

```json
{
  "listen_addr": ":8080",
  "db_path": "./data/stats.db",
  "watch": true,
  "sources": [
    {
      "name": "my-website",
      "type": "file",
      "path": "/var/log/nginx/access.log",
      "format": { "engine": "nginx", "preset": "combined" }
    }
  ]
}
```

白話解釋：

- `listen_addr` — 儀表板使用的埠號（`:8080` → http://localhost:8080）
- `db_path` — 統計資料的儲存位置（就是一個檔案）
- `watch` — 持續讀取新增的日誌內容
- `sources` — 要讀取的日誌檔。上面是 Nginx 的標準設定；Apache 請改用
  `"engine": "apache"`。

編輯後重新啟動程式，流量就會出現在儀表板上。附帶註解的完整範例請見
[`config.example.json`](config.example.json)，所有選項的說明請見
[設定參考文件](docs/CONFIGURATION.md)（英文）。

### 匯入既有的舊日誌

監看模式只會讀取*新增*的內容。想載入既有的歷史日誌，執行一次：

```bash
./web-req-attr-linux-amd64 -import /var/log/nginx/access.log
```

## 啟用登入

預設情況下（尚未建立任何帳號時），任何打得開頁面的人都能使用儀表板——
在自己電腦上沒問題，但放上公開伺服器**不行**。要強制登入，請在第一次
啟動前於 `config.json` 加入：

```json
"auth": {
  "require_account": true
}
```

如果沒有設定密碼，程式會為 `admin` 使用者產生一組隨機密碼並輸出在啟動
訊息中——用它登入後，再到「**使用者**」分頁修改密碼。更多細節請見
[設定參考文件](docs/CONFIGURATION.md#authentication-auth)。

## 部署為常駐服務

想在伺服器上長期執行，**[部署教學](docs/DEPLOYMENT.md)**（英文）提供
每種方式的逐步說明。Docker 的快速版本：

```bash
git clone https://github.com/moehoshio/WebRequestAttribution.git
cd WebRequestAttribution
cp config.example.json config.json   # 修改成指向你的日誌
docker-compose up -d
```

教學中也涵蓋不使用 Docker 的方式（systemd）、用 HTTPS 反向代理保護
儀表板，以及如何安全地更新。

## 常見問題

**我的資料會外流嗎？**
不會。所有資料都存放在本機的單一 SQLite 檔案。唯一可選的對外查詢是
世界地圖用的 IP → 國家解析（可在「設定」中關閉）。

**我的日誌格式比較特殊，能解析嗎？**
可以。除了標準的 Nginx/Apache 格式外，也能用自訂 pattern 描述任意格式。
請見[設定參考文件](docs/CONFIGURATION.md#custom-formats)。

**世界地圖顯示「尚無地理位置資料」。**
位置是背景逐步解析的——第一次匯入後等幾分鐘再看看。

## 文件

| 文件 | 內容 |
|---|---|
| [部署教學](docs/DEPLOYMENT.md) | Docker、systemd、HTTPS、更新、疑難排解 |
| [設定參考](docs/CONFIGURATION.md) | `config.json` 所有選項的完整說明 |
| [開發者指南](docs/DEVELOPMENT.md) | 從原始碼編譯、HTTP API、專案內部結構 |
| [貢獻指南](CONTRIBUTING.md) | 如何貢獻程式碼 |

## 授權

MIT License
