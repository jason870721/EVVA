# RP-15 — WebAPI 認證硬化（兌現 minted-token TODO）

> 狀態：**草案 / Draft（待 Johnny 拍板）** ｜ 日期：2026-06-10 ｜ 波次：**第四波（運營硬化）** ｜ 優先：P1
> 觸發：2026-06-10 健康檢查——`service.go:58` 自己標的 TODO（「非 loopback 暴露前要換
> minted token」）至今未兌現；webhook endpoint 刻意免 token（RP-9 的 loopback 信任）。
> 本機單人用沒問題；**任何 reverse-proxy / 區網暴露前，本 RP 是硬前置。**
> 上層：[`../health-check-2026-06-10.md`](../health-check-2026-06-10.md) ｜ 前文：[RP-9](RP-9-external-event-webhook.md)（免 token 是當時明示的測試期 tradeoff）

---

## 1. 現況盤點（file:line 證據）

| # | 事實 | 位置 | 意義 |
| --- | --- | --- | --- |
| S1 | 預設綁 `127.0.0.1:8888`（invariant #6 的安全基線） | `service/service.go:49-52` | ✅ loopback 邊界是今天唯一的防線 |
| S2 | guard token 存在但屬「test-convenience tradeoff」 | `service/service.go:57,228` | ⚠️ 弱（可預期/可繞） |
| S3 | webhook `POST /api/swarm/{id}/event` 刻意不掛 guard | `webapi/api.go:317-323` | ⚠️ loopback 上任何進程可喚醒 leader |
| S4 | 真 TODO：minted token | `service/service.go:58` | 本 RP 的存在理由 |

## 2. 設計方向

1. **Minted token**：service 啟動時生成（`common.GenUUID` 級隨機），寫
   `~/.evva/service/token`（0600）；CLI（`evva swarm …`）與 FE 自動讀取——loopback
   單機體驗**不變**（人不用打 token）。
2. **非 loopback 顯式 opt-in**：`--addr` 非 127.0.0.1 時拒絕啟動，除非同時給
   `--allow-remote`；此時所有 endpoint（含 WS、含靜態頁之外的 API）強制 token。
3. **Webhook secret（向後相容）**：`settings.webhook_secret` 設了才驗（header
   `X-Evva-Webhook-Secret` 或 HMAC-SHA256 簽 body 二選一，傾向先做 shared-secret
   header——Sunday `events.post` 加一個 header 即可）；不設則維持 loopback 信任。
4. 文件：user-guide 加「把工作站掛到區網/外網」一節，明示威脅模型（agents hold only
   HTTP；token 即全權——等同 operator）。

## 3. 驗收（DoD）

1. loopback 預設流程零變化（CLI/FE 不用人工配 token）。
2. `--addr 0.0.0.0:8888` 無 `--allow-remote` → 啟動即拒絕；有 → 無 token 的請求一律 401。
3. 設定 `webhook_secret` 後，無/錯 secret 的 event POST 一律 401，Sunday 帶 secret 正常；
   未設定時行為與現狀一致。
4. token 檔權限 0600；輪換 = 重啟（v1 不做線上輪換）。

## 4. 非目標

- 多使用者/RBAC、TLS 終結（交給 reverse proxy）、OAuth——單 operator 模型不變。
