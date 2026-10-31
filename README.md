# v2node

基於 [gunzi-666/v2node](https://github.com/gunzi-666/v2node) 的節點服務端。保留 Go 1.27.1、Xray 26.9.30、gunzi 的入口多 IP 修正及 AnyTLS UDP 空閒回收，加入 AnyTLS 內建 HTTP 回應與 GitHub 反代下載功能。

核心從 gunzi 的 `0548acaedda6` 提交派生：[Alma0010/Xray-core](https://github.com/Alma0010/Xray-core/tree/dc3983bb490c622570c4cbdf74ec5b717672383a)。配套 [REALITY 相容修改](https://github.com/Alma0010/REALITY/commit/167dfca01cf1599fc22d5a61a9be6cce78afa057)接受有效的傳統 X25519 ClientHello，不再強制要求 ML-KEM；金鑰認證及 VLESS UUID 驗證保留。`go.mod` 同時固定這兩個衍生模組。

需要搭配支援 v2node API 的面板，例如 [修改版 V2Board](https://github.com/wyx2685/v2board)。

## 安裝與更新

以 root 執行。GitHub 直連安裝：

```bash
curl -fL https://raw.githubusercontent.com/Alma0010/v2node/main/script/install.sh -o /tmp/v2node-install.sh && bash /tmp/v2node-install.sh --direct
```

使用 `v4.gh-proxy.org` 反代加速：

```bash
curl -fL https://v4.gh-proxy.org/https://raw.githubusercontent.com/Alma0010/v2node/main/script/install.sh -o /tmp/v2node-install.sh && bash /tmp/v2node-install.sh --proxy
```

反代線路涵蓋安裝腳本、管理腳本與節點發行版壓縮包。成功安裝後會把所選線路保存至 `/etc/v2node/download-proxy`，後續更新會沿用：

```bash
v2node update
v2node update --proxy
v2node update --direct
v2node update v1.2.3 --proxy
v2node update_shell
```

`v1.2.3` 僅為版本格式示例，請替換成此倉庫實際存在的 Release。也可用 `--proxy=https://你的反代/` 指定其他 HTTPS 反代前綴。

版本查詢使用 GitHub API；腳本和壓縮包下載使用所選線路。發行版會先解析成具體標籤，再下載 `/releases/download/<標籤>/...`，不依賴反代的 `latest/download` 路徑。API 無法連線時，可在安裝或更新命令中指定版本，跳過版本查詢。

安裝時，管理腳本也從同一個版本標籤下載，與安裝包保持一致。

### v0.4.8 正式版

此版本使用 gunzi 的最新主程式與 Xray 26.9.30，包含 AnyTLS 404、反代下載及 REALITY 舊 ClientHello 相容修改。

```bash
curl -fL https://v4.gh-proxy.org/https://raw.githubusercontent.com/Alma0010/v2node/main/script/install.sh -o /tmp/v2node-install.sh && bash /tmp/v2node-install.sh v0.4.8 --proxy && v2node update_shell --proxy
```

此命令使用目前的安裝／管理腳本，節點程式固定為 v0.4.8。

### REALITY 舊客戶端相容

上游 REALITY 的 `8cdf7bf9c7f0` 會拒絕未帶 `X25519MLKEM768` 的 ClientHello。此分支只將這個條件放寬為必須存在有效的 X25519 或混合金鑰資料，支援 ML-KEM 的客戶端仍可協商，不修改其他認證流程。

因此 mihomo v1.19.31 使用 `chrome` 指紋時，既有未設定 `support-x25519mlkem768` 的節點也可連線；有啟用該選項的客戶端同樣受支援。REALITY 認證失敗仍回落到配置的目標站；REALITY 通過但 VLESS UUID 錯誤時，則維持原有的拒絕行為。

此修改接受較舊的 ClientHello 格式，不會新增重放防護。跨客戶端測試使用隔離的 mihomo v1.19.31，覆蓋預設設定、ML-KEM 設定、普通 HTTPS 回落、錯誤 REALITY 金鑰及錯誤 VLESS UUID；所有流量都只在 loopback 上轉發。

安裝時可同時提供面板參數：

```bash
bash /tmp/v2node-install.sh --proxy --api-host https://panel.example.com --node-id 1 --api-key YOUR_API_KEY
```

更新會先完成下載、壓縮包檢查和管理腳本語法檢查，再替換程式；下載失敗時保留舊程式，已有 `/etc/v2node/config.json` 不會被覆蓋。

安裝腳本會重建服務啟動檔；有自訂啟動參數或服務設定時，請先備份並核對這些設定。

## Alpine／OpenRC 更新

Alpine 3.21 的更新流程會先完成下載與檢查，再停止舊程序、替換執行檔並重新啟動，避免 OpenRC 仍追蹤已被替換的舊程序。已存在的 `config.json` 會保留。

若曾在更新後看到 `has already been started` 與 `status: crashed`，請使用上面的目前安裝腳本重裝指定版本；v0.4.8 標籤內的舊安裝腳本沒有這項修正。執行 `v2node update_shell --proxy` 可取得支援 Alpine 日誌的新管理腳本。

OpenRC 會把標準輸出與錯誤保存至 `/var/log/v2node.log`，可用 `v2node log` 或 `tail -n 100 /var/log/v2node.log` 查看。`crashed` 也可能是程序確實退出，仍需依日誌判斷原因。

## AnyTLS 內建 404 回應

AnyTLS 使用 TCP 且啟用 TLS 時，預設對未通過認證的普通 HTTP/1.0、HTTP/1.1 請求回傳 nginx 樣式的 `404 Not Found`。不需要安裝 Nginx、開回落端口或準備網站。

- 在原有 TLS 連線內回應，沿用節點現有證書及面板證書下發。
- 狀態行為 `HTTP/1.1 404 Not Found`，包含 `Server: nginx`、GMT `Date`、`Content-Type: text/html`、正確的 `Content-Length` 和 `Connection: close`。
- GET 等請求收到固定 nginx 樣式 HTML；HEAD 的 Content-Length 相同，但不傳送頁面內容。不回顯請求的路徑、Host 或其他資料。
- 此模式只協商 `http/1.1`，避免將 HTTP/1.x 回應發到已協商 HTTP/2 的連線。
- 正確密碼繼續使用原有 AnyTLS 代理流程；HTTP 探測不進入已認證的用戶路由。
- 請求頭讀取上限為 32 KiB，沿用認證超時，限制 128 個並發認證，回應寫入超時為 2 秒。成功認證後即釋放認證名額，不限制已建立的代理會話數量。
- 無法解析的二進位、不完整或超大請求會關閉連線；這是固定 HTTP 回應功能，不保證與 Nginx 的所有網路指紋相同，也不新增重放防護。

如需關閉，在對應 `Nodes` 配置中加入：

```json
"AnyTLSRejectHTTP": false
```

其他傳輸方式維持原有行為；明確設為 `true` 時，僅接受 AnyTLS + TCP + TLS 的配置。

## 構建與驗證

```bash
GOEXPERIMENT=jsonv2 go build -v -o build_assets/v2node -trimpath -ldflags "-X 'github.com/wyx2685/v2node/cmd.version=$version' -s -w -buildid="
GOEXPERIMENT=jsonv2 go test ./...
python3 -m unittest discover -s tests -v
```

若要在 Linux/Windows amd64 本地執行 mihomo 跨客戶端測試，先下載固定校驗值的測試核心，再設定測試程式路徑。Linux 範例：

```bash
python3 tests/fetch_mihomo.py --output .local/test-tools/mihomo
V2NODE_MIHOMO_TEST_BINARY="$PWD/.local/test-tools/mihomo" V2NODE_REQUIRE_MIHOMO_TEST=1 GOEXPERIMENT=jsonv2 go test ./core -run TestMihomoRealityCompatibility -v
```

GitHub Actions 會強制執行這組測試；下載內容的 SHA-256 不符時不會執行測試核心。

在 GitHub Actions 開啟 **Build and Release → Run workflow**，選擇 `main`：

- `version` 留空：只測試及構建，產生 Artifacts。
- 填入新的版本號，例如 `v1.2.3`：測試與全部平台構建成功後，自動建立對應標籤及 Release，上傳安裝包後才正式發布。程式內的版本號也使用此版本。
- 已存在的版本號會提前拒絕，不覆蓋已發布的 Release。若上傳失敗而留下草稿，可用該次執行的 Re-run failed jobs 繼續上傳。
- 帶 `-beta.1` 等後綴的版本會發布成 Pre-release，不設為 Latest；一般安裝命令的 `latest` 會取得正式版。

仍可手動建立 Release，讓工作流程構建並附加安裝包。安裝與更新均使用 `Alma0010/v2node` 的發行版。
