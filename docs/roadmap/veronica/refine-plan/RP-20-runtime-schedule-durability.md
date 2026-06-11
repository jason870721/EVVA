# RP-20 — Runtime 排程持久化（schedule_set 要活過 service 重啟）

> 狀態：**草案 / Draft（待 Johnny 拍板）** ｜ 階段：**第五波** ｜ 優先：**P0（bug 等級——承諾與實作矛盾）** ｜ 日期：2026-06-11
> 觸發：Sunday swarm 重整時 source review 發現（尚未在生產咬人，但必然會）：leader 用 `schedule_set` 調過的節奏，service 一重啟就**靜默回滾到 manifest 版**。
> 關聯：[RP-7](RP-7-leader-scheduled-wake.md)（建出 schedule_set 的那一波，本文補它的持久層）、[RP-8](RP-8-web-agent-schedule-mgmt.md)（Web 端改排程同樣受惠）、user-guide「重启与续跑」一節（目前的承諾本文使其成立）
> 請求者：Sunday。**無 Sunday-specific code。**

---

## 1. Problem（observed，含 file:line 證據）

三個事實拼起來就是 bug：

1. `SetMemberSchedule`（`internal/swarm/space.go:330-338`）只轉呼叫 in-memory supervisor 的 `SetSchedule`——**沒有任何 store 寫入**。
2. `internal/swarm/store/migrations/` 只有 `0001_init` / `0002_message_claim` / `0003_message_idempotency`——**沒有 schedule 表**。
3. 重啟重建走 `Loader.BuildAll`（`internal/swarm/agentdef/loader.go:123-152`），註解明言 *"Manifest schedule is authoritative over the agent's profile.yml"*——排程**只**從磁碟 manifest 來。

而對外的承諾恰好相反：

- `leaderProtocol`（`internal/swarm/teamprompt.go`）把 `schedule_set` 教成 leader 的 standing-duties 工具；
- user guide §8「重启与续跑」承諾「你什么都不用做——它自然续跑」；
- **同類功能 alarm 是 durable 的**（`space.go:351-355`：*"Durable alarms persist beside the space store and are re-armed"*）——排程與鬧鐘一持久一揮發，連 operator 都料不到。

實際後果：Sunday 的 friday 在行情關鍵期把 analyst-flow 從 10m 調到 5m → 半夜 service 重啟（或 crash + launchd 拉起，RP-18 之後這是常態）→ 節奏無聲退回 10m。**leader 不知道、operator 不知道、analyst 更不知道。**

## 2. Proposal

1. **新 migration `0004_schedules.sql`**：`schedules(member TEXT PRIMARY KEY, cron TEXT, prompt TEXT, cleared INTEGER NOT NULL DEFAULT 0, updated_at TEXT)`。
2. **寫路徑**：`SetMemberSchedule` / `ClearMemberSchedule` 先落表（clear 寫 tombstone `cleared=1`，不是刪行——「清掉了」這個事實也要活過重啟），再進 supervisor。
3. **重建優先序**（restart 路徑）：member 有 runtime 列 → 用它（含 tombstone = 無排程）；沒有 → 用 manifest/profile（現行為）。
4. **重新註冊（`evva swarm .`）= operator 明示意圖**：re-register 時**清空該 space 的 runtime 排程列**，回到 manifest 權威。這是「我改了 manifest，請以它為準」的天然出口，不需要新 CLI。
5. `list_members` 的班表顯示（RP-7 已有）標注來源：`cron "*/5 * * * *" (runtime, set 2026-06-11)` vs `(manifest)`——leader 與 operator 一眼可分。

## 3. Why evva（not Sunday）

排程的持久語義是 swarm runtime 的契約。Sunday 唯一的替代方案是教 friday「每次重啟後重新 schedule_set 一遍」——但成員根本**不知道** service 重啟過，這條路不存在。

## 4. Acceptance

- `schedule_set` → `evva service stop` → `start`：節奏與 prompt 不變，`list_members` 標 `(runtime)`。
- `schedule_clear` → 重啟：該成員仍無排程（tombstone 生效，不回退 manifest）。
- `evva swarm stop` + `evva swarm .` 重註冊：runtime 列清空，manifest 班表生效。
- 純 manifest 的 space（從未 runtime 改過）行為與今日完全相同。
- migration 對既有 `.vero/vero.db` 無損升級；`-race` 全綠。

## 5. Notes

- 表以 space 的 store 為界（每 space 一 db），天然隔離——不需要 space id 欄位。
- 與 RP-8（Web 改排程）共用同一寫路徑：Web 的 SetSchedule 也經 `SetMemberSchedule`，落表即同享持久化。
- 順手：排程變更（誰、改了誰、cron、來源）記一條 RP-17 event log——「昨晚誰把 watchdog 調成 5 分鐘」應該一句 grep 可答。
