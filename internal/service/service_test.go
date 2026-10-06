package service

import (
	"testing"
	"time"

	"theater-costume/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Actor{}, &models.Costume{}, &models.Performance{},
		&models.Assignment{}, &models.CareRecord{}, &models.Incident{},
		&models.InventoryCheck{}, &models.RecastLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mkCostume(t *testing.T, db *gorm.DB, code string, status string, change, clean int) models.Costume {
	t.Helper()
	c := models.Costume{Code: code, Name: "礼服-" + code, Status: status,
		ChangeSeconds: change, CleanSeconds: clean, CurrentLocation: "主换装间"}
	if status == models.CostumeAvailable {
		when := time.Now().Add(-72 * time.Hour)
		c.LastCleanedAt = &when
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("create costume: %v", err)
	}
	return c
}

func mkActor(t *testing.T, db *gorm.DB, name string) models.Actor {
	t.Helper()
	a := models.Actor{Name: name, Role: name + "的角色"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatalf("create actor: %v", err)
	}
	return a
}

func mkPerf(t *testing.T, db *gorm.DB) models.Performance {
	t.Helper()
	p := models.Performance{Name: "测试场", ShowTime: time.Now().Add(24 * time.Hour), Status: "scheduled"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatalf("create perf: %v", err)
	}
	return p
}

func hasCode(cs []Conflict, code string) bool {
	for _, c := range cs {
		if c.Code == code {
			return true
		}
	}
	return false
}

// TestConflict_CleanInterval 同一件衣服给不同演员共用，转场够但清洁间隔不足 → 拦截。
func TestConflict_CleanInterval(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	p := mkPerf(t, db)
	c := mkCostume(t, db, "C1", models.CostumeAvailable, 120, 1800)
	a1 := mkActor(t, db, "演员甲")
	a2 := mkActor(t, db, "演员乙")
	base := p.ShowTime

	// 甲先穿，正常建单。
	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a1.ID, Sequence: 1, Scene: "一幕",
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	}); err != nil {
		t.Fatalf("first assignment should pass: %v", err)
	}

	// 乙 25 分钟后接手：转场 5 分钟（>120s 转场缓冲），但清洁要求 30 分钟 → clean 冲突。
	_, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a2.ID, Sequence: 2, Scene: "二幕",
		HandoverAt: base.Add(25 * time.Minute), NeedByAt: base.Add(30 * time.Minute),
		ReturnAt: base.Add(50 * time.Minute),
	})
	ce, ok := IsConflict(err)
	if !ok {
		t.Fatalf("期望清洁间隔冲突被拦截，实际 err=%v", err)
	}
	if !hasCode(ce.Conflicts, ConflictClean) {
		t.Fatalf("期望 clean 冲突码，得到 %+v", ce.Conflicts)
	}

	// 间隔放宽到 31 分钟后应通过。
	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a2.ID, Sequence: 2, Scene: "二幕",
		HandoverAt: base.Add(51 * time.Minute), NeedByAt: base.Add(56 * time.Minute),
		ReturnAt: base.Add(70 * time.Minute),
	}); err != nil {
		t.Fatalf("间隔足够时应放行，实际 %v", err)
	}
}

// TestConflict_Transfer 衣服还没交回，下一位就要用 → 转场冲突，衣服不能及时到位。
func TestConflict_Transfer(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	p := mkPerf(t, db)
	c := mkCostume(t, db, "C1", models.CostumeAvailable, 120, 60)
	a1 := mkActor(t, db, "演员甲")
	a2 := mkActor(t, db, "演员乙")
	base := p.ShowTime

	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a1.ID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	// 甲 20:00 才交回，乙 20:01 就要签收（转场缓冲 120 秒）→ transfer 冲突。
	_, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a2.ID, Sequence: 2,
		HandoverAt: base.Add(21 * time.Minute), NeedByAt: base.Add(26 * time.Minute),
		ReturnAt: base.Add(40 * time.Minute),
	})
	ce, ok := IsConflict(err)
	if !ok || !hasCode(ce.Conflicts, ConflictTransfer) {
		t.Fatalf("期望转场冲突，得到 %v", err)
	}
}

// TestConflict_InsertBetween 回归：新任务插在两件已有任务之间，
// 冲突归属到“前一棒”时也必须拦住新建（related_ids 覆盖前后两棒）。
func TestConflict_InsertBetween(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	p := mkPerf(t, db)
	c := mkCostume(t, db, "C1", models.CostumeAvailable, 120, 60)
	a1 := mkActor(t, db, "演员甲")
	a2 := mkActor(t, db, "演员乙")
	a3 := mkActor(t, db, "演员丙")
	base := p.ShowTime

	// 已有两棒：甲 base~base+30；乙 base+80 接手（间隔 50 分钟，无冲突）。
	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a1.ID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a2.ID, Sequence: 3,
		HandoverAt: base.Add(80 * time.Minute), NeedByAt: base.Add(85 * time.Minute),
		ReturnAt: base.Add(100 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	// 丙在 base+31 插入：甲 base+30 才交回，转场仅 1 分钟 < 2 分钟缓冲。
	_, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a3.ID, Sequence: 2,
		HandoverAt: base.Add(31 * time.Minute), NeedByAt: base.Add(36 * time.Minute),
		ReturnAt: base.Add(60 * time.Minute),
	})
	ce, ok := IsConflict(err)
	if !ok {
		t.Fatalf("插入中间造成转场冲突应被拦截")
	}
	if !hasCode(ce.Conflicts, ConflictTransfer) {
		t.Fatalf("期望 transfer 冲突，得到 %+v", ce.Conflicts)
	}
}

// TestConflict_ActorRush 同一演员连续上场来不及换装。
func TestConflict_ActorRush(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	p := mkPerf(t, db)
	c1 := mkCostume(t, db, "C1", models.CostumeAvailable, 120, 1800)
	c2 := mkCostume(t, db, "C2", models.CostumeAvailable, 600, 1800)
	a := mkActor(t, db, "忙碌的主角")
	base := p.ShowTime

	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c1.ID, ActorID: a.ID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	// 20:20 下场，20:23 必须再上场，而 C2 换装需要 600 秒（10 分钟）→ actor_rush。
	_, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c2.ID, ActorID: a.ID, Sequence: 2,
		HandoverAt: base.Add(20 * time.Minute), NeedByAt: base.Add(23 * time.Minute),
		ReturnAt: base.Add(40 * time.Minute),
	})
	ce, ok := IsConflict(err)
	if !ok || !hasCode(ce.Conflicts, ConflictActorRush) {
		t.Fatalf("期望演员赶场冲突，得到 %v / %+v", err, ce)
	}
}

// TestConflict_ChangeTime 签收后到上场之间不够换一件衣服的时间。
func TestConflict_ChangeTime(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	p := mkPerf(t, db)
	c := mkCostume(t, db, "C1", models.CostumeAvailable, 600, 1800)
	a := mkActor(t, db, "演员甲")
	base := p.ShowTime

	_, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a.ID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(2 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	})
	ce, ok := IsConflict(err)
	if !ok || !hasCode(ce.Conflicts, ConflictChangeTime) {
		t.Fatalf("期望换装耗时冲突，得到 %v", err)
	}
}

// TestRecast_PendingThenSignedLock 临时换角只作用于未执行任务；签收后锁定原演员。
func TestRecast_PendingThenSignedLock(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	p := mkPerf(t, db)
	c := mkCostume(t, db, "C1", models.CostumeAvailable, 120, 1800)
	a1 := mkActor(t, db, "原定演员")
	a2 := mkActor(t, db, "替身演员")
	base := p.ShowTime

	as, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a1.ID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	// pending 阶段换角成功，PlannedActorID 保留原演员。
	out, _, err := svc.Recast(as.ID, &RecastInput{ActorID: a2.ID, Reason: "原定演员失声"})
	if err != nil {
		t.Fatalf("pending 任务应允许换角: %v", err)
	}
	if out.ActorID != a2.ID || out.PlannedActorID != a1.ID {
		t.Fatalf("换角后当前演员应为替身、原定演员仍保留，得到 actor=%d planned=%d",
			out.ActorID, out.PlannedActorID)
	}
	var logs int64
	db.Model(&models.RecastLog{}).Where("assignment_id = ?", as.ID).Count(&logs)
	if logs != 1 {
		t.Fatalf("应留下 1 条换角审计记录，得到 %d", logs)
	}

	// 替身签收。
	if _, err := svc.Scan(&ScanInput{Code: c.Code, Action: "sign"}); err != nil {
		t.Fatalf("签收失败: %v", err)
	}

	// 已签收任务再换角 → 拒绝，且记录仍关联实际签收人（替身）。
	_, _, err = svc.Recast(as.ID, &RecastInput{ActorID: a1.ID, Reason: "想改回去"})
	ce, ok := IsConflict(err)
	if !ok || !hasCode(ce.Conflicts, ConflictSigned) {
		t.Fatalf("已签收任务换角应被拦截，得到 %v", err)
	}
	var after models.Assignment
	db.First(&after, as.ID)
	if after.ActorID != a2.ID {
		t.Fatalf("签收记录必须仍关联实际签收演员 %d，实际 %d", a2.ID, after.ActorID)
	}
	if after.SignedActorID == nil || *after.SignedActorID != a2.ID {
		t.Fatal("SignedActorID 应锁定为实际签收人")
	}
}

// TestBlock_AssignReturnedCostume 散场交回未经洗护的衣服不能再次分配，
// 洗护完成前持续拦截，完成洗护后放行。
func TestBlock_AssignReturnedCostume(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	p := mkPerf(t, db)
	c := mkCostume(t, db, "C1", models.CostumeReturned, 120, 1800)
	a := mkActor(t, db, "演员甲")
	base := p.ShowTime

	_, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a.ID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	})
	ce, ok := IsConflict(err)
	if !ok || !hasCode(ce.Conflicts, ConflictUnwashed) {
		t.Fatalf("未经洗护再次分配应被拦截，得到 %v", err)
	}

	// 扫码签收同样应被拦截。
	if _, err := svc.Scan(&ScanInput{Code: c.Code, Action: "lookup"}); err != nil {
		t.Fatalf("扫码查询不应报错: %v", err)
	}

	// 走正规洗护流程：开始洗护 → 完成洗护 → 恢复可用后才能排单。
	rec, err := svc.StartCare(c.ID, &StartCareInput{Type: "wash", Operator: "洗衣组"})
	if err != nil {
		t.Fatalf("开始洗护失败: %v", err)
	}
	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a.ID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	}); err == nil {
		t.Fatal("洗护进行中仍应拦截分配")
	}
	updated, err := svc.CompleteCare(rec.ID, "洗衣组")
	if err != nil {
		t.Fatalf("完成洗护失败: %v", err)
	}
	if updated.Status != models.CostumeAvailable {
		t.Fatalf("洗护完成后状态应为 available，得到 %s", updated.Status)
	}
	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: c.ID, ActorID: a.ID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	}); err != nil {
		t.Fatalf("洗护恢复可用后应放行，得到 %v", err)
	}
}

// TestCheckout_LostAndDamaged 散场清点少件/破损必须开处理单，
// 处理前衣服不可用；找回仍需洗护，修复完成才恢复可用，不允许补数量。
func TestCheckout_LostAndDamaged(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	p := mkPerf(t, db)
	lost := mkCostume(t, db, "C-LOST", models.CostumeReturned, 120, 1800)
	damaged := mkCostume(t, db, "C-DMG", models.CostumeReturned, 120, 1800)
	a1 := mkActor(t, db, "演员甲")
	a2 := mkActor(t, db, "演员乙")
	base := p.ShowTime

	// 少件。
	r1, err := svc.CheckOne(p.ID, &CheckItem{CostumeID: lost.ID, Found: false, Note: "下场后斗篷不见"})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Incident == nil || r1.Incident.Type != models.IncidentLost {
		t.Fatalf("少件应自动开 lost 事件单")
	}
	got, err := svc.GetCostume(lost.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.CostumeLost {
		t.Fatalf("少件后服装状态应为 lost，得到 %s", got.Status)
	}
	// 少件状态不能直接送去洗护。
	if _, err := svc.StartCare(lost.ID, &StartCareInput{Type: "wash"}); err == nil {
		t.Fatal("有未处理少件事件时不应允许洗护")
	}
	// 重复清点被拒绝，不能靠再点一次把问题点没。
	if _, err := svc.CheckOne(p.ID, &CheckItem{CostumeID: lost.ID, Found: true}); err == nil {
		t.Fatal("同场重复清点必须被拒绝")
	}
	// 找回 → 仍须洗护（returned），不能直接变回可用。
	_, upd, err := svc.ResolveIncident(r1.Incident.ID, &ResolveIncidentInput{
		Resolution: "found", Handler: "服装主管", Note: "在侧廊衣架找回"})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Status != models.CostumeReturned {
		t.Fatalf("找回后应处于待洗护 returned，得到 %s", upd.Status)
	}
	// 未洗护仍拦截排单。
	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: lost.ID, ActorID: a1.ID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	}); err == nil {
		t.Fatal("找回但未洗护，仍应拦截")
	}

	// 破损：开单 → 修复完成恢复可用。
	r2, err := svc.CheckOne(p.ID, &CheckItem{CostumeID: damaged.ID, Found: true, Condition: "damaged", Note: "袖口撕裂"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Incident == nil || r2.Incident.Type != models.IncidentDamaged {
		t.Fatal("破损应自动开 damaged 事件单")
	}
	_, d2, err := svc.ResolveIncident(r2.Incident.ID, &ResolveIncidentInput{
		Resolution: "repaired", Handler: "维修组"})
	if err != nil {
		t.Fatal(err)
	}
	if d2.Status != models.CostumeAvailable {
		t.Fatalf("维修检验合格后应恢复 available，得到 %s", d2.Status)
	}
	// 修复后的衣服可以正常排单。
	if _, _, err := svc.CreateAssignment(&AssignmentInput{
		PerformanceID: p.ID, CostumeID: damaged.ID, ActorID: a2.ID, Sequence: 2,
		HandoverAt: base.Add(60 * time.Minute), NeedByAt: base.Add(65 * time.Minute),
		ReturnAt: base.Add(80 * time.Minute),
	}); err != nil {
		t.Fatalf("维修完成后应可排单: %v", err)
	}
}

// TestIncidentList_Preload 回归：事件单列表必须能带出服装信息
// （Incident 需定义 Costume 关联，否则 GORM Preload 直接报错）。
func TestIncidentList_Preload(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	p := mkPerf(t, db)
	c := mkCostume(t, db, "C-X", models.CostumeReturned, 120, 1800)
	if _, err := svc.CheckOne(p.ID, &CheckItem{CostumeID: c.ID, Found: false}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListIncidents("open")
	if err != nil {
		t.Fatalf("事件单列表不应报错: %v", err)
	}
	if len(list) != 1 || list[0].Costume.Code != "C-X" {
		t.Fatalf("应返回 1 条带服装信息的事件单，得到 %+v", list)
	}
}
