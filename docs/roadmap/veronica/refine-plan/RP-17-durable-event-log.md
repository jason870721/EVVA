# RP-17 — Durable event log ＋ 最小 metrics（事後可查、可量化）

> 狀態：**草案 / Draft（待 Johnny 拍板）** ｜ 日期：2026-06-10 ｜ 波次：**第四波（運營硬化）** ｜ 優先：P2
> 觸發：2026-06-10 健康檢查——事件流只進 WS（看過即逝），`gateTracker` 只為 pending 權限門
> 做 reconnect 重放；事後 forensics 只能翻成員 transcript。**「昨晚 03:00 發生了什麼」
> 今天答不出來。** 同時 supervisor 的 wake/run 生命週期只有 log 行，沒有可聚合的數字。
> 上層：[`../health-check-2026-06-10.md`](../health-check-2026-06-10.md) ｜ 解鎖：[EX-4](../explore/EX-4-replay-eval-harness.md)（replay/eval 的資料地基）

---

## 1. 現況盤點（file:line 證據）

| # | 事實 | 位置 | 意義 |
| --- | --- | --- | --- |
| S1 | `pump` → `publish` → `hub.Publish`，事件不落地 | `service/service.go:713-742` | 看過即逝 |
| S2 | `gateTracker` 只留 pending gates（reconnect 重放用） | `service/service.go:136-192` | 非歷史記錄 |
| S3 | spaceSink 對 out chan 是阻塞回壓（1024 buffer） | `space.go:88-91,510-517` | ⚠️ 旁路寫**不可**再引入阻塞 |
| S4 | wake/run 生命週期已有結構化 log 行 | `scheduler.go`（Debug/Warn 各處） | 改成 counter 即 metrics |
| S5 | run 時長/abort 與否在 `runOnce` 一處可量 | `scheduler.go:182-200` | 量測點集中 |

## 2. 設計方向

### 2.1 Event log：publish 旁路、永不阻塞

`publish` 在 `hub.Publish` 旁寫一份到 per-space append-only log：

- 形態：`<workdir>/.vero/events/YYYY-MM-DD.jsonl`（日切；輪轉/保留沿用 RP-16 的
  `retention_days`）。內容即 wireEvent JSON（已在手上，零重組）。
- **絕不回壓 pump**：buffered writer ＋ 滿則丟並計數（`events_dropped`）。事件日誌是
  輔助觀測，丟幾條可接受；凍住 pump 不可接受（S3 的教訓，RP-2 §3.5 同款哲學）。
- 可關：`settings.event_log: false`。

### 2.2 Metrics：counter 起步，不拉依賴

service 維護 per-space/per-member 計數（atomic）：wakes{timer,message,event}、runs、
aborts、run 時長直方圖（粗桶即可：<10s / <1m / <10m / ≥10m）、mail hint dropped、
events_dropped。出口：

- `GET /api/swarm/{id}/metrics` 回 JSON（guard 後）；
- `/healthz` 加 uptime 與 space 數（RP-18 一併）。

不引 Prometheus 依賴；要接的話 JSON→exporter 是使用者側的事。

## 3. 驗收（DoD）

1. 重啟後可以回答「昨晚 03:00 哪個成員醒了、跑了多久、有沒有 abort」（翻 jsonl 即可）。
2. 壓測（fake 高頻事件）下 pump 不變慢、agent Emit 不阻塞；滿載時 `events_dropped` 計數
   上升而非凍結。
3. metrics endpoint 數字與測試注入的事件數一致。
4. `event_log: false` 時零 IO、零行為變化。

## 4. 非目標

- 全文檢索/查詢 API（翻檔案＋jq 夠用）；
- tracing/spans；
- 跨 space 聚合儀表板（FE 後續再議）。
