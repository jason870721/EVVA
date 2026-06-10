# RP-18 — Ops 收口（cron 方言文件化、daemon 自動重啟、healthz 擴充）

> 狀態：**草案 / Draft（待 Johnny 拍板）** ｜ 日期：2026-06-10 ｜ 波次：**第四波（運營硬化）** ｜ 優先：P2
> 觸發：2026-06-10 健康檢查的三個小缺口，單獨都不值一個 RP，收口在一起。
> 上層：[`../health-check-2026-06-10.md`](../health-check-2026-06-10.md)

---

## 1. cron 方言文件化

自寫 cron（`agentdef/schedule.go:86-93`）支援 `*`、`*/n`、`n`、`a-b`、`a-b/n`、逗號清單，
dom/dow 雙限定時為標準 OR 語意；**不支援**秒級欄位、`L`/`W`/`#` 特殊符、`@every`/`@daily`
別名、TZ 欄位（時區語意＝系統本地，已在 v1.4.5-beta.2 寫進 `schedule_set` 工具描述與
Environment 時區行）。

- 落點：user-guide（zh/en）各加一節「schedule 方言」；`evva-swarm.yml` 範例註解同步。
- 順手：`parseCron` 對不支援語法的錯誤訊息點名「不支援 L/W/#/秒級」，少一輪猜。

## 2. daemon 自動重啟模板

service 有 pidfile/log（SPRD-1-9）但 crash 後**不會自己回來**（`cmd/evva/swarm.go:212`
只有手動 `evva swarm run`）。重啟後的恢復路徑（resume.go：session、mail requeue、
membership、alarms）已可靠——缺的只是「有人把它拉起來」。

- 提供 launchd plist（macOS）與 systemd unit（Linux）模板，`KeepAlive`/`Restart=on-failure`；
  放 `docs/user-guide/` 並在 README 連結。
- 可選甜頭：`evva service install-unit` 偵測平台、寫模板、印啟用指令（不自動啟用）。

## 3. /healthz 擴充

現況只回 200（`webapi/api.go:263`）。加：版本、uptime、space 數、各 space 成員數與
frozen 數——一行 curl 能看出「活著但空轉」vs「正常服役」。與 RP-17 的 metrics endpoint
互補（healthz 免 token、不含敏感細節）。

## 4. 驗收（DoD）

1. user-guide 兩語言都有 schedule 方言節；錯誤訊息測試覆蓋不支援語法。
2. 模板在乾淨機器上照文件操作：kill -9 service 後 30 秒內自動回來，swarm 成員恢復
   （resume 路徑既有測試保障）。
3. `/healthz` 回 JSON 含上述欄位，無 token 可讀，不洩漏成員名以外的內容。
