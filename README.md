# 剧院演出服装换装路线与场次清点系统

演出前，服装师按角色和场次安排服装交接；系统提前发现**演员来不及换装**和**衣服不能及时到位**的问题；
散场逐件清点，少件/破损必须走问题处理流程，洗护/维修完成前戏服不可再分配。

- 后端：Go + Gin + GORM（SQLite，纯 Go 驱动，无需 CGO）
- 前端：Vue 3（本地 vendor，离线可用，无需 npm 构建）
- 扫码：本地接口模拟（选任务 + 填条码）
- 启动：一条命令 `./run.sh`

## 快速开始

```bash
./run.sh          # 编译并启动，默认 http://localhost:8080
./run.sh test     # 运行全部验证测试
ADDR=:9090 ./run.sh
```

首次启动自动写入演示剧目《夜宴》（4 演员 / 4 角色 / 4 场次 / 6 件戏服 / 6 个换装任务）。
重置演示数据：`POST /api/seed`。

## 页面导览

| 页面 | 能看到 / 能做什么 |
|---|---|
| 场次看板 | 冲突总览；按上场时间排列的换装路线（谁、饰什么、穿哪件、哪个换装点）；每件戏服**当前位置、持有人、下一位使用人及上场时间** |
| 换装安排 | 新建任务前可**冲突预检**；软性冲突（时间紧张）可强制安排并留痕，硬性冲突直接拒绝；临时换角 |
| 模拟扫码 | 扫码签收（任务冻结、记录实际签收人）、扫码交回（回到服装间）；条码不符拒绝 |
| 戏服资产 | 每件实物一个编号，登记预计换装时间；**没有库存数量字段** |
| 洗护维修 | 送洗护/维修（戏服锁定）、完成后恢复可用 |
| 问题单 | 少件/破损上报（锁定戏服）、必须填写找回/修复经过才能结单 |
| 散场清点 | 逐件标记 在场/少件/破损；少件破损自动生成问题单 |

## 冲突检测规则

对每条换装任务（上场时刻 `OnAt`、下场时刻 `OffAt`、换装点、预计换装时间）：

1. **演员时间重叠**（硬）：同一演员两个上场区间重叠。
2. **演员来不及换装**：上一场下场后，换装时间不足；换装点不同还要加上剧目设定的转场时间。
3. **戏服时间重叠**（硬）：同一件实物在两个重叠区间分给不同人。
4. **戏服不能及时到位**：上一处交回后到下一处换装开始之间，换装点转场时间不足；
   不同角色共用同一件时，还必须满足剧目设定的清洁间隔（默认 15 分钟）。
5. **戏服未恢复可用**（硬）：存在未完成的洗护/维修，或未处理的少件/破损问题单。

硬性冲突不允许保存；时间类软性冲突可勾选强制安排，冲突说明写入任务备注并显示在场次页面。

## 关键业务规则

- **临时换角只改未签收任务**：`POST /api/recast` 按角色替换 `pending` 任务的演员；
  已签收/已交回任务保留 `originalActorId`，签收记录关联实际签收人（`signedByActorId`），均不被改写。
- **未经洗护/问题未处理再次分配的拦截**：创建任务与扫码签收两个入口都检查未完成的洗护/维修和未决问题单。
- **散场清点**：标少件/破损自动开问题单并锁定戏服；结单必须填写处理说明，不能靠"补数量"消除问题（模型里根本不存在数量字段）。
- 已签收任务不能改路线、不能重复签收；已交回后才能送洗护。

## 主要接口

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/tasks/check` | 冲突预检（不落库） |
| POST | `/api/tasks` | 安排换装任务（`{"force":true}` 可保留软性冲突强制安排） |
| PUT/DELETE | `/api/tasks/:id` | 修改未执行任务 / 取消任务 |
| POST | `/api/recast` | 临时换角 `{showId, roleId, newActorId}` |
| POST | `/api/scan/sign` | 扫码签收 `{taskId, code}` |
| POST | `/api/scan/return` | 扫码交回 `{taskId, code}` |
| POST/GET | `/api/care`、`/api/care/:id/finish` | 洗护/维修送出与完成 |
| POST/GET | `/api/issues`、`/api/issues/:id/resolve` | 少件/破损问题单与结单（需处理说明） |
| POST/GET | `/api/checks`、`/api/checks/:id/items` | 散场清点与逐件登记 |
| GET | `/api/shows/:id/board` | 场次看板（路线、冲突、戏服位置/下一人） |

## 验证

```bash
./run.sh test
```

覆盖（均已通过）：

- `TestActorChangeConflict` —— 演员下场后来不及换装（含换装点转场）的检出与创建拦截；
- `TestCostumeCleaningConflict` —— 同一件戏服不同角色共用、转场+清洁间隔不足的拦截；
- `TestCostumeOverlapHardBlock` —— 同一件戏服时间重叠，硬性冲突强制也不允许；
- `TestRecastKeepsSignedTasks` —— 临时换角只动未签收任务，已签收记录仍关联原演员与实际签收人；
- `TestBlockedUntilCareFinished` —— 洗护未完成时分配被拦截，完成洗护恢复可用后放行；
- `TestIssueLifecycle` —— 破损问题单未结单拦截分配、空说明不能结单；
- `TestCheckMissingCreatesIssue` —— 散场清点少件自动开问题单并锁定戏服；
- HTTP 端到端：`TestHTTPCareGate`、`TestHTTPRecast`、`TestHTTPCheckIssueFlow`。

## 目录结构

```
cmd/server/main.go          入口（静态托管前端 + API）
internal/models/models.go   GORM 模型（戏服/任务/洗护/问题单/清点）
internal/service/           冲突引擎、业务规则、看板、种子数据
internal/api/               Gin 路由与处理器
web/                        Vue 3 单页（vendor/vue 本地文件）
data/theater.db             SQLite（运行后生成，已在 .gitignore）
```
