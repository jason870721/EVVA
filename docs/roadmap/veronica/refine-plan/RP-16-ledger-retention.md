# RP-16 — Ledger retention（messages / tasks 的歸檔與瘦身）

> 狀態：**草案 / Draft（待 Johnny 拍板）** ｜ 日期：2026-06-10 ｜ 波次：**第四波（運營硬化）** ｜ 優先：P1
> 觸發：2026-06-10 健康檢查——設計言明「v1 never deletes history」（`space.go` removeAgent
> 註解），messages/tasks 只增不刪。週級沒事；**月級的 24/7 集群**（Sunday：watchdog 一天
> 720 醒、每醒至少一輪 mail）會讓全量掃描的 API 與 web 載入逐漸變慢。
> 上層：[`../health-check-2026-06-10.md`](../health-check-2026-06-10.md) ｜ 前文：[RP-6](RP-6-completed-task-scaling.md)（已做 task 分頁＋active 預設，本 RP 是它的「物理刪除」續集）

---

## 1. 現況盤點（file:line 證據）

| # | 事實 | 位置 | 意義 |
| --- | --- | --- | --- |
| S1 | messages 表只增（read_at/claimed_at 標記、無刪除原語） | `store/messages.go` | 增長無界 |
| S2 | tasks 同（RP-6 加了分頁/計數，未刪） | `store/tasks.go`、`service.go:880` | 同上 |
| S3 | `Messages` API 仍是近況全掃 | `service/service.go:950` | 變慢的第一現場 |
| S4 | Close 時 WAL TRUNCATE checkpoint 已做 | `store/store.go:85-95` | ✅ 檔案層乾淨 |
| S5 | sqlite 本身百萬行無壓力 | — | 痛點在 API/UI 層，不是 DB 引擎 |

## 2. 設計方向

1. **Retention 規則（保守預設）**：
   - messages：`read_at` 非空 **且** 超過 `retention_days`（預設 30）→ 可清。
   - tasks：`completed` 且超期 → 可清（連同其 verify_note）。
   - **絕不動**：unread、claimed、`pending/running/suspended/verifying` 的任務。
2. **歸檔而非蒸發**：清理前先 dump 成 `<workdir>/.vero/archive/YYYY-MM.jsonl.gz`
   （append、可重讀），然後 DELETE ＋ 週期 `VACUUM`/checkpoint。要查古早史就翻歸檔檔。
3. **入口**：
   - 手動：`evva swarm vacuum <ref> [--days N] [--dry-run]`（dry-run 印將清理的行數）。
   - 自動：service 每日 local 凌晨跑一次（時區語意沿 `pkg/common` 約定）；
     `settings.retention_days: 0` = 完全關閉（保留今天的「never deletes」行為）。
4. webapi 加 `POST /api/swarm/{id}/vacuum`（guard 後）供 FE 一鍵。

## 3. 驗收（DoD）

1. `--dry-run` 數字與實清一致；活資料（unread/claimed/active task）清理前後逐 byte 不變。
2. 歸檔檔可重讀（一個小 reader 測試）；清理後 unread 收發、claim/settle、task 狀態機
   全部既有測試綠燈。
3. `retention_days: 0` 時零行為變化。
4. 月級資料量模擬（10 萬 messages）下 `Messages` API 延遲回到常數級。

## 4. 非目標

- 跨 space 聚合歸檔、壓縮格式可調、線上重建索引——都不做，保持一個檔案一個月的樸素形態。
