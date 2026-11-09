# v2node

基於 [gunzi-666/v2node](https://github.com/gunzi-666/v2node) 的節點服務端，加入 **AnyTLS 內建 404、GitHub 反代下載**，保留上游的入口多 IP 修正與 AnyTLS UDP 空閒回收。REALITY 舊客戶端相容採用 [wyx2685/REALITY](https://github.com/wyx2685/REALITY) 的上游實作。

需搭配支援 v2node API 的面板，例如 [修改版 V2Board](https://github.com/wyx2685/v2board)。

[安裝](#安裝) · [更新與版本](#更新與版本) · [常用命令](#常用命令) · [功能說明](#功能說明) · [啟動排錯](#啟動排錯) · [開發與發佈](#開發與發佈)

## 安裝

以 **root** 執行。以下命令使用目前的安裝／管理腳本，節點程式從本倉庫的最新正式 Release 下載。

**使用 `v4.gh-proxy.org` 反代：**

```bash
curl -fL \
  https://v4.gh-proxy.org/https://raw.githubusercontent.com/Alma0010/v2node/main/script/install.sh \
  -o /tmp/v2node-install.sh &&
bash /tmp/v2node-install.sh --proxy &&
v2node update_shell --proxy
```

<details>
<summary>GitHub 直連安裝</summary>

```bash
curl -fL \
  https://raw.githubusercontent.com/Alma0010/v2node/main/script/install.sh \
  -o /tmp/v2node-install.sh &&
bash /tmp/v2node-install.sh --direct &&
v2node update_shell --direct
```

</details>

首次安裝可依提示設定面板；已有 `/etc/v2node/config.json` 會保留。安裝腳本會重建服務啟動檔，有自訂啟動參數或服務設定時，請先備份並核對。

<details>
<summary>首次安裝時傳入面板參數</summary>

先下載上方安裝腳本，再執行：

```bash
bash /tmp/v2node-install.sh --proxy \
  --api-host https://panel.example.com \
  --node-id 1 \
  --api-key YOUR_API_KEY
```

這些參數用於首次建立配置；已有配置不會被覆蓋。完成後執行 `v2node update_shell --proxy` 同步目前的管理腳本。

</details>

## 更新與版本

更新節點後，同步目前的管理腳本：

```bash
v2node update && v2node update_shell
```

兩個命令均沿用先前保存的下載線路。也可指定線路或版本：

| 操作 | 命令 |
| --- | --- |
| 使用反代更新 | `v2node update --proxy && v2node update_shell --proxy` |
| 使用直連更新 | `v2node update --direct && v2node update_shell --direct` |
| 更新至指定版本 | `v2node update v0.4.9 --proxy && v2node update_shell --proxy` |
| 只更新管理腳本 | `v2node update_shell` |

版本號請使用本倉庫 [Releases](https://github.com/Alma0010/v2node/releases) 中實際存在的版本。若需要重新安裝指定版本，使用上方下載的目前安裝腳本：

```bash
bash /tmp/v2node-install.sh v0.4.9 --proxy && v2node update_shell --proxy
```

安裝時，管理腳本會先從對應的 Release 標籤下載，並自動修復舊版更新問題；`update_shell` 再同步 `main` 的維護修正。v0.4.9 已包含 Alpine／OpenRC 與 GeoIP／Geosite 安裝修正；從 v0.4.8 重新安裝時，請使用本文開頭的目前安裝腳本。

### v0.4.9 更新

- 同步 gunzi 上游至 `77bd360`，Xray 核心更新至 26.10.31，Go 更新至 1.27.3，預設關閉 MPTCP。
- REALITY 使用上游 `25ea3c0` 的 X25519 相容實作，同時支援傳統及 ML-KEM ClientHello。
- 合併 wyx 上游至 `129abe1`，修正 SS2022 預設使用者金鑰長度與 Freedom 出口的 DNS 策略設定位置。
- 保留 AnyTLS 404、下載反代、UDP 空閒回收、入口多 IP 及 Alpine／資料檔修正。

### 下載線路

- `--proxy` 使用 `https://v4.gh-proxy.org/`；`--direct` 使用 GitHub 直連。也可用 `--proxy=https://proxy.example/` 指定其他 HTTPS 反代前綴。
- 安裝／更新成功後，線路保存至 `/etc/v2node/download-proxy`。反代涵蓋安裝腳本、管理腳本與節點壓縮包。
- 版本查詢使用 GitHub API，解析具體標籤後再下載 `/releases/download/<標籤>/...`，不依賴反代的 `latest/download` 路徑。API 無法連線時，指定版本即可跳過查詢。
- 更新會先完成下載、壓縮包檢查及管理腳本語法檢查，再替換程式；下載或檢查失敗時保留舊程式。

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `v2node` | 開啟管理選單 |
| `v2node status` | 查看服務狀態 |
| `v2node start` / `v2node stop` | 啟動／停止服務 |
| `v2node restart` | 重啟服務 |
| `v2node log` | 查看執行日誌 |
| `v2node version` | 查看已安裝的程式版本 |
| `v2node enable` / `v2node disable` | 啟用／取消開機自啟 |
| `v2node generate` | 重新生成配置 |
| `v2node uninstall` | 卸載節點程式 |

### 配置與檔案

| 路徑 | 用途 |
| --- | --- |
| `/etc/v2node/config.json` | 節點配置，更新時保留 |
| `/usr/local/v2node/v2node` | 節點執行檔 |
| `/usr/local/v2node/geoip.dat`、`geosite.dat` | Xray 預設讀取的資料檔 |
| `/etc/v2node/geoip.dat`、`geosite.dat` | 保留給既有明確指定的資料檔路徑 |
| `/etc/v2node/download-proxy` | 保存的下載線路 |
| `/var/log/v2node.log` | 新版 OpenRC 啟動檔捕捉的標準輸出與錯誤 |

## 功能說明

### AnyTLS 內建 404

AnyTLS 使用 **TCP + TLS** 時，預設對未通過認證的正常 HTTP/1.0、HTTP/1.1 請求回傳 nginx 樣式的 `404 Not Found`，無需額外部署網站或 Nginx。

- 沿用節點現有證書與面板證書下發，正確密碼繼續使用 AnyTLS 代理流程。
- GET 回傳固定頁面；HEAD 的 `Content-Length` 相同，但沒有頁面內容。回應包含 `Server: nginx`、GMT `Date`、`Content-Type` 和 `Connection: close`。
- 此模式只協商 `http/1.1`，不回顯請求中的路徑、Host 或其他資料。

如需關閉，在對應的 `Nodes` 項目中加入 `"AnyTLSRejectHTTP": false`，保留其他欄位。

<details>
<summary>認證與連線限制</summary>

請求頭上限為 32 KiB，沿用原有認證超時；同時最多處理 128 個認證，回應寫入超時為 2 秒。成功認證後即釋放名額，不限制已建立的代理會話數量。

無法解析的二進位、不完整或超大請求會關閉連線。其他傳輸方式維持原有行為；明確設為 `true` 時，僅接受 AnyTLS + TCP + TLS 配置。

</details>

此功能提供固定 HTTP 回應，TLS 握手與異常請求處理仍可能有別於真正 Nginx，也不新增重放防護。

### REALITY 舊客戶端相容

採用 [wyx2685/REALITY 上游實作](https://github.com/wyx2685/REALITY/commit/25ea3c0fa2550b72ee2ba20fd8d70c9a6789b274)，接受有效的傳統 X25519 或混合金鑰 ClientHello，不再強制要求 `X25519MLKEM768`，保留金鑰認證及 VLESS UUID 驗證。

mihomo v1.19.31 使用 `chrome` 指紋時，未設定或已啟用 `support-x25519mlkem768` 的情況均有跨客戶端測試。REALITY 認證失敗仍回落到配置的目標站；REALITY 通過但 VLESS UUID 錯誤時，維持原有拒絕行為。

## 啟動排錯

先查看狀態與日誌：

```bash
v2node status
v2node log
```

### Alpine／OpenRC 顯示 crashed

`crashed` 可能是程序退出，也可能涉及服務狀態，需要依日誌判斷。新版安裝腳本會在下載檢查完成後停止舊程序、替換執行檔並重新啟動，亦可處理舊程序被替換或已退出後留下的狀態。

使用本文開頭的目前安裝腳本重新安裝所需版本，配置會保留。若舊管理腳本提示不支援 Alpine 日誌，先執行 `v2node update_shell`；新版服務啟動檔的日誌也可直接用 `tail -n 100 /var/log/v2node.log` 查看。

### 找不到 geoip.dat／geosite.dat

若日誌出現 `failed to open geoip.dat` 或載入 `geoip:cn` 失敗，請確認資料檔存在於 `/usr/local/v2node`。目前安裝腳本會同時補齊程式目錄與 `/etc/v2node` 的資料檔，重新安裝所需版本即可修復。

<details>
<summary>需要前台查看啟動輸出時</summary>

先停止後台服務，避免重複程序佔用端口：

```bash
v2node stop
cd /usr/local/v2node
./v2node server --config /etc/v2node/config.json
```

完成排查後按 `Ctrl+C` 結束前台程序，再執行 `v2node start` 恢復後台服務。

</details>

## 開發與發佈

### 本地構建與測試

使用 `go.mod` 指定的 Go 版本；目前為 Go 1.27.3、Xray 26.10.31。

```bash
build_version=dev
GOEXPERIMENT=jsonv2 go build -trimpath \
  -ldflags "-X github.com/wyx2685/v2node/cmd.version=$build_version -s -w -buildid=" \
  -o build_assets/v2node .
GOEXPERIMENT=jsonv2 go test ./... github.com/xtls/xray-core/proxy/anytls
python3 -m unittest discover -s tests -v
```

<details>
<summary>mihomo 與 Alpine 整合測試</summary>

mihomo 跨客戶端測試支援 Linux／Windows amd64。以下為 Linux 範例，測試核心下載時會核對固定的 SHA-256：

```bash
python3 tests/fetch_mihomo.py --output .local/test-tools/mihomo
V2NODE_MIHOMO_TEST_BINARY="$PWD/.local/test-tools/mihomo" \
  V2NODE_REQUIRE_MIHOMO_TEST=1 GOEXPERIMENT=jsonv2 \
  go test ./core -run TestMihomoRealityCompatibility -v
```

測試覆蓋預設 ClientHello、ML-KEM、普通 HTTPS 回落、錯誤 REALITY 金鑰與錯誤 VLESS UUID，流量僅在 loopback 轉發。

Alpine 3.21 的服務更新與資料檔載入測試需要 Docker、Go、curl 及 unzip：

```bash
bash tests/test_alpine_openrc.sh
```

GitHub Actions 會強制執行上述整合測試。

</details>

### GitHub Actions 發佈

開啟 **Actions → Build and Release → Run workflow**，選擇 `main` 並填寫 `version`：

| `version` 輸入 | 執行結果 |
| --- | --- |
| 留空 | 測試與構建，產生 Artifacts |
| `v1.2.3` | 全部測試與平台構建成功後，自動建立標籤及正式 Release，設為 Latest |
| `v1.2.3-rc.1` | 發佈為 Pre-release，不設為 Latest |

版本號僅為格式示例。已存在的標籤或 Release 會提前拒絕；附件全部上傳後才公開 Release。上傳失敗留下的草稿，可從該次執行的 **Re-run failed jobs** 繼續處理。

也可手動建立新的 Release，觸發同一流程構建並附加安裝包。

### 上游與核心來源

| 元件 | 本分支使用的來源 |
| --- | --- |
| 節點主程式 | [gunzi-666/v2node `77bd360`](https://github.com/gunzi-666/v2node/commit/77bd36014f437b02359bc22a2ee263bc3288d4b9)，另合併 [wyx2685/v2node `129abe1`](https://github.com/wyx2685/v2node/commit/129abe1a3db0d8f314e1c21a30a9d41fc42ecfc1) |
| Xray 核心 | [Alma0010/Xray-core `ea4d630`](https://github.com/Alma0010/Xray-core/tree/ea4d630a8526c248225ac793e7f75e798e0bb984)，基於 gunzi 的 `b9c76d7565ff` |
| REALITY | [wyx2685/REALITY `25ea3c0`](https://github.com/wyx2685/REALITY/commit/25ea3c0fa2550b72ee2ba20fd8d70c9a6789b274) |

核心衍生模組的版本固定於 [`go.mod`](go.mod)。
