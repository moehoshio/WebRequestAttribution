# Web Request Attribution

🌐 [English](README.md) | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md) | **日本語**

あなたのサイトに誰が来ているかを見える化——データは第三者に送られず、すべて手元に残ります。

Web Request Attribution は Web サーバー（Nginx / Apache）のアクセスログを
読み取り、見やすいダッシュボードに変換します：アクセス推移、人気ページ、
ブラウザ、ステータスコード、リクエスト送信元の世界地図など。すべての機能が
組み込まれた小さなプログラム 1 つだけ——データベースサーバーも、別途の
フロントエンドも、追加のインストールも不要です。

## スクリーンショット

| ダークモード | ライトモード |
|:-:|:-:|
| ![Dark Mode](docs/screenshot-dark.png) | ![Light Mode](docs/screenshot-light.png) |

## 特長

- 🚀 **単一ファイル・依存なし** — プログラムを 1 つダウンロードするだけで動作
- 📊 **内蔵ダッシュボード** — ブラウザを開けば統計がすぐ見られる
- 🗺️ **世界地図** — リクエストがどの国・地域から来ているかを表示（無料・API キー不要）。バブルにマウスを乗せると詳細を表示
- 📡 **リアルタイム更新** — 新しいログが自動的にダッシュボードへ反映
- 🔍 **強力なフィルター** — IP、ページ、ドメイン、ブラウザ、OS、ステータスコード、キーワード、日付で絞り込み。値の先頭に `!` を付けると除外（複数の除外条件に対応）
- 🔐 **ログインは任意** — 手元の PC ではオープンに、サーバー上ではアカウント必須に
- 🌐 **4 言語対応** — 日本語、English、繁體中文、简体中文
- 🐳 **Docker 対応** — コマンド 1 つでデプロイ

## 3 ステップで開始

**1. ダウンロード**：[最新リリース](https://github.com/moehoshio/WebRequestAttribution/releases/latest)からお使いのシステムに合うファイルを入手：

| システム | ファイル |
|---|---|
| Linux（x86_64） | `web-req-attr-linux-amd64` |
| Linux（ARM、Raspberry Pi など） | `web-req-attr-linux-arm64` |
| macOS（Intel） | `web-req-attr-darwin-amd64` |
| macOS（Apple Silicon） | `web-req-attr-darwin-arm64` |
| Windows | `web-req-attr-windows-amd64.exe` |

**2. 実行**（Linux/macOS。Windows は `.exe` をダブルクリックするだけ）：

```bash
chmod +x web-req-attr-linux-amd64
./web-req-attr-linux-amd64
```

**3. ブラウザで** <http://localhost:8080> を開く。完了！🎉

初回起動時に `config.json` が自動作成されます。どのログファイルを監視
するか、どのキーワードを追跡するかは、ブラウザの「**設定**」タブから
そのまま設定できます。

## ログファイルを指定する

サイトのトラフィックを分析するには、アクセスログの場所を教えるだけです。
ブラウザの「**設定**」タブからログソースを追加するか、`config.json` を
編集します：

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

各項目の意味：

- `listen_addr` — ダッシュボードのポート（`:8080` → http://localhost:8080）
- `db_path` — 統計データの保存先（1 つのファイル）
- `watch` — 新しいログ行を継続的に読み取る
- `sources` — 読み取るログファイル。上記は Nginx の標準設定。Apache の
  場合は `"engine": "apache"` に変更してください。

編集後にプログラムを再起動すると、トラフィックがダッシュボードに表示
されます。コメント付きの完全な例は
[`config.example.json`](config.example.json)、全オプションの説明は
[設定リファレンス](docs/CONFIGURATION.md)（英語）をご覧ください。

### 既存の過去ログを取り込む

監視モードは*新しく追記された*行のみを読み取ります。既存の履歴ログを
取り込むには、一度だけ実行します：

```bash
./web-req-attr-linux-amd64 -import /var/log/nginx/access.log
```

## ログインを有効にする

初期状態（アカウント未作成）では、ページを開ける人なら誰でも
ダッシュボードを操作できます——手元の PC なら問題ありませんが、公開
サーバーでは**危険**です。ログインを必須にするには、初回起動の前に
`config.json` へ追加します：

```json
"auth": {
  "require_account": true
}
```

パスワードを設定しなかった場合、`admin` ユーザーのランダムなパスワードが
生成され、起動ログに出力されます——それでサインインし、「**ユーザー**」
タブから変更してください。詳細は
[設定リファレンス](docs/CONFIGURATION.md#authentication-auth)を参照。

## サービスとしてデプロイする

サーバーで常時稼働させる方法は **[デプロイガイド](docs/DEPLOYMENT.md)**
（英語）にステップごとの説明があります。Docker での簡易版：

```bash
git clone https://github.com/moehoshio/WebRequestAttribution.git
cd WebRequestAttribution
cp config.example.json config.json   # 自分のログを指すように編集
docker-compose up -d
```

ガイドでは Docker を使わない方法（systemd）、HTTPS リバースプロキシでの
保護、安全なアップデート方法も扱っています。

## よくある質問

**データが外部に送られることはありますか？**
ありません。すべてローカルの単一 SQLite ファイルに保存されます。唯一の
任意の外部問い合わせは世界地図用の IP → 国解決です（「設定」でオフに
できます）。

**特殊なログ形式でも解析できますか？**
できます。標準の Nginx/Apache 形式に加え、カスタムパターンで任意の形式を
記述できます。[設定リファレンス](docs/CONFIGURATION.md#custom-formats)を
参照してください。

**世界地図に「位置情報はまだありません」と表示されます。**
位置はバックグラウンドで順次解決されます——初回インポート後、数分待って
からご確認ください。

## ドキュメント

| ドキュメント | 内容 |
|---|---|
| [デプロイガイド](docs/DEPLOYMENT.md) | Docker、systemd、HTTPS、更新、トラブルシューティング |
| [設定リファレンス](docs/CONFIGURATION.md) | `config.json` 全オプションの説明 |
| [開発者ガイド](docs/DEVELOPMENT.md) | ソースからのビルド、HTTP API、内部構造 |
| [コントリビューション](CONTRIBUTING.md) | コードへの貢献方法 |

## ライセンス

MIT License
