# 剧院演出服装换装路线与场次清点系统

演出前，服装师按角色和场次安排服装交接，提前发现 **演员来不及换装**、**衣服不能及时到位**、
**同一件衣服不同演员共用时清洁间隔不足** 等问题；演出中扫码签收/交回；散场逐件清点，
**洗护或维修完成后才能恢复可用**，少件/破损必须开处理事件单，不能靠“补库存数量”抹平。

- 后端：Go + Gin + GORM + SQLite（资产与场次记录持久化）
- 前端：Vue 3 + Vite（构建后由 Go `//go:embed` 内嵌，单进程、单端口交付）
- 扫码：`/api/scan` 本地模拟扫码枪（输入/点选条码即可）

## 一条命令启动

```bash
./run.sh
```

脚本会自动：构建前端 → 编译后端（内嵌前端）→ 打开 SQLite 并写入演示数据 → 启动。

启动后访问 **http://localhost:8080**

- 自定义端口：`ADDR=:9090 ./run.sh`
- 前端热更新开发：`./run.sh --dev`（Vite 5173，API 代理到 8080）
- 也可直接：`go build -o theater-costume . && ./theater-costume -addr :8080 -db data/theater.db`

首次启动会在 `data/theater.db` 写入一场演示演出，其中**刻意内置三类冲突**，打开“场次路线”即可看到：

| 冲突 | 演示 |
| --- | --- |
| 演员来不及换装 | 林岚第二幕 20:05 下场、20:09 必须上场，斗篷换装需 5 分钟 |
| 清洁间隔不足 | C001 晚礼服 20:05 由林岚交回，20:20 转给苏晓，短于 30 分钟清洁要求 |
| 未经洗护再分配 | C003 女仆裙当前“已交回待洗护”，却被排进第三幕，场次页直接报红、建单接口硬拦截 |

## 业务规则（系统强制执行）

1. **每件实物独立编号**（`Costume.Code` 唯一），登记预计换装耗时与最短清洁间隔。
2. **换装路线**：交接任务按上场顺序排列，记录换装点、签收时间、上场 deadline、交回时间。
3. **建单三重校验**（冲突返回 HTTP 409 与结构化明细）：
   - 签收→上场是否够换好这一件（`change_time`）；
   - 同一件实物衣服前一位交回→下一位签收的**转场缓冲**（默认 120 秒，`transfer`）；
   - 同一件衣服不同演员共用时的**清洁间隔**（`clean`）；
   - 同一演员上场时间重叠 / 连续上场来不及换装（`actor_overlap` / `actor_rush`）。
4. **状态门控**：衣服只有 `available` 能被分配/签收；`returned`（待洗护）、`washing`、`repairing`、
   `lost`、`damaged`、`retired` 一律拦截。洗护或维修**完成**是恢复可用的唯一入口。
5. **临时换角**：仅 `pending` 任务可改演员，并写 `RecastLog` 审计；已签收任务永久锁定实际签收人
   （`SignedActorID`），接口返回 `signed_locked` 拒绝。
6. **散场清点**：一件衣服同场只能清点一次。
   - 完好 → `returned` 送洗护队列；
   - 破损 → 自动开 `damaged` 事件单，衣服转维修；
   - 未找到 → 自动开 `lost` 事件单。
7. **事件处理不等于补数量**：找回后仍须洗护；修复检验合格才恢复可用；无法修复则报废。
   系统不存在“调整库存数量”这一操作。

## 主要 HTTP 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/performances/:id` | 场次页：每件服装实时位置、下一位使用人、全部冲突、清点记录 |
| POST | `/api/assignments` | 安排换装交接（冲突 409；`"force": true` 可主管确认强排） |
| POST | `/api/assignments/:id/recast` | 临时换角（已签收 409 锁定） |
| POST | `/api/scan` | 模拟扫码：`action=lookup/sign/collect` |
| POST | `/api/costumes/:id/care` | 开始洗护 `wash` / 维修 `repair` |
| POST | `/api/care/:id/complete` | 完成洗护/维修 → 恢复可用 |
| POST | `/api/performances/:id/check` | 散场逐件清点（少件/破损自动开事件单） |
| GET/POST | `/api/incidents`、`/api/incidents/:id/resolve` | 事件单处理：`found/repaired/written_off` |

## 验证（三类拦截均有自动化测试）

```bash
go test ./...
```

覆盖：清洁间隔冲突、转场不到位、演员赶场、换装耗时、临时换角（pending 可改 / 已签收锁定并保留原记录）、
未经洗护再分配的建单与扫码拦截、洗护完成后放行、散场少件/破损事件全流程，以及 HTTP 409 结构化响应。

### curl 快速手测（启动后）

```bash
# 未经洗护的 C003 再分配 → 409 unwashed
curl -s -XPOST localhost:8080/api/assignments -H 'Content-Type: application/json' -d '{
  "performance_id":1,"costume_id":3,"actor_id":3,"sequence":9,
  "handover_at":"2026-10-11T20:00:00+08:00","need_by_at":"2026-10-11T20:05:00+08:00",
  "return_at":"2026-10-11T20:30:00+08:00"}'

# 扫码查询 C001 的当前位置与下一位使用人
curl -s -XPOST localhost:8080/api/scan -H 'Content-Type: application/json' -d '{"code":"C001","action":"lookup"}'
```

## 目录结构

```
main.go                     入口：建库、种子数据、路由、内嵌前端托管
internal/models/models.go   GORM 模型（演员/服装/场次/交接/洗护/事件/清点/换角日志）
internal/store/db.go        SQLite 初始化、迁移、演示数据
internal/service/           业务规则：冲突检测、换角、扫码、洗护门控、散场清点
internal/api/handlers.go    Gin REST 接口
web/                        Vue 3 前端（场次路线 / 服装资产 / 扫码交接 / 散场清点）
```
