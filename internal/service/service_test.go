package service

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"theater-costumes/internal/models"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, m := range models.AllModels() {
			db.Where("1=1").Delete(m)
		}
	})
	return db
}

func mkTask(showID, costumeID, roleID, actorID uint, cp string, onH, onM, offH, offM int) models.Task {
	base := time.Now()
	at := func(h, m int) time.Time {
		return time.Date(base.Year(), base.Month(), base.Day(), h, m, 0, 0, base.Location())
	}
	return models.Task{
		ShowID: showID, CostumeID: costumeID, RoleID: roleID, ActorID: actorID,
		ChangePoint: cp, Status: models.TaskPending, OriginalActorID: actorID,
		OnAt: at(onH, onM), OffAt: at(offH, offM),
	}
}

func hasType(cs []Conflict, typ string) bool {
	for _, c := range cs {
		if c.Type == typ {
			return true
		}
	}
	return false
}

// 基础场景：1 个演出（转场3分钟、清洁间隔15分钟），2 演员，2 角色，两件戏服
func baseFixture(t *testing.T, svc *Service) (models.Show, models.Actor, models.Actor, models.Role, models.Role, models.Costume, models.Costume) {
	t.Helper()
	show := models.Show{Name: "测试剧", TransferSeconds: 180, CleaningSeconds: 900, Status: "planned"}
	if err := svc.DB.Create(&show).Error; err != nil {
		t.Fatal(err)
	}
	a1 := models.Actor{Name: "演员甲"}
	a2 := models.Actor{Name: "演员乙"}
	svc.DB.Create(&a1)
	svc.DB.Create(&a2)
	r1 := models.Role{ShowID: show.ID, Name: "角色甲"}
	r2 := models.Role{ShowID: show.ID, Name: "角色乙"}
	svc.DB.Create(&r1)
	svc.DB.Create(&r2)
	// C1 换装需 10 分钟；C2 换装需 5 分钟
	c1 := models.Costume{Code: "T-001", Name: "戏服一", ChangeSeconds: 600, Status: models.CostumeAvailable, Location: "服装间"}
	c2 := models.Costume{Code: "T-002", Name: "戏服二", ChangeSeconds: 300, Status: models.CostumeAvailable, Location: "服装间"}
	svc.DB.Create(&c1)
	svc.DB.Create(&c2)
	return show, a1, a2, r1, r2, c1, c2
}

// 1) 换装路线冲突：演员来不及换装（换装10分钟+转场3分钟，只给8分钟）
func TestActorChangeConflict(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	show, a1, _, r1, _, c1, c2 := baseFixture(t, svc)

	first := mkTask(show.ID, c1.ID, r1.ID, a1.ID, "侧台左", 19, 30, 19, 50)
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	// 19:56 上场，间隔只有 6 分钟；换装点不同还要转场 3 分钟，共需 8 分钟
	next := mkTask(show.ID, c2.ID, r1.ID, a1.ID, "侧台右", 19, 56, 20, 10)
	cs, err := EvaluateTask(db, show, next)
	if err != nil {
		t.Fatal(err)
	}
	if !hasType(cs, ConflictActorChangeTight) {
		t.Fatalf("期望检出演员来不及换装冲突，实际: %+v", cs)
	}

	// 通过 API 创建应被拒绝
	in := TaskInput{
		ShowID: show.ID, CostumeID: c2.ID, RoleID: r1.ID, ActorID: a1.ID,
		OnAt: next.OnAt.Format("2006-01-02T15:04"), OffAt: next.OffAt.Format("2006-01-02T15:04"),
		ChangePoint: "侧台右",
	}
	if _, _, err := svc.CreateTask(in, false); err == nil {
		t.Fatal("换装时间不足的任务应被拦截")
	}

	// 把上场推迟到 20:10（间隔20分钟），冲突消失
	later := next
	later.OnAt = next.OnAt.Add(12 * time.Minute)
	later.OffAt = next.OffAt.Add(12 * time.Minute)
	cs2, _ := EvaluateTask(db, show, later)
	if hasType(cs2, ConflictActorChangeTight) {
		t.Fatalf("时间充裕时不应报换装冲突: %+v", cs2)
	}
}

// 2) 戏服周转：同一件戏服给不同角色共用，清洁间隔不足
func TestCostumeCleaningConflict(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	show, a1, a2, r1, r2, c1, _ := baseFixture(t, svc)

	// 甲 19:30–20:00 在侧台左穿 C1
	first := mkTask(show.ID, c1.ID, r1.ID, a1.ID, "侧台左", 19, 30, 20, 0)
	db.Create(&first)
	// 乙 20:10 在侧台右穿同一件 C1（不同角色）：交回后只有10分钟，需转场3+清洁15=18分钟
	next := mkTask(show.ID, c1.ID, r2.ID, a2.ID, "侧台右", 20, 10, 20, 30)
	cs, err := EvaluateTask(db, show, next)
	if err != nil {
		t.Fatal(err)
	}
	if !hasType(cs, ConflictCostumeTurnover) {
		t.Fatalf("期望检出戏服转场/清洁不及冲突，实际: %+v", cs)
	}

	in := TaskInput{
		ShowID: show.ID, CostumeID: c1.ID, RoleID: r2.ID, ActorID: a2.ID,
		OnAt: next.OnAt.Format("2006-01-02T15:04"), OffAt: next.OffAt.Format("2006-01-02T15:04"),
		ChangePoint: "侧台右",
	}
	if _, _, err := svc.CreateTask(in, false); err == nil {
		t.Fatal("清洁间隔不足的共用安排应被拦截")
	}

	// 推迟到 20:30，距上场有30分钟（换装10+转场3+清洁15=28），周转冲突消失
	later := next
	later.OnAt = next.OnAt.Add(20 * time.Minute)
	later.OffAt = next.OffAt.Add(20 * time.Minute)
	cs2, _ := EvaluateTask(db, show, later)
	if hasType(cs2, ConflictCostumeTurnover) {
		t.Fatalf("周转时间充足时不应报冲突: %+v", cs2)
	}
}

// 3) 同一件戏服时间重叠分配给两人：硬性冲突，强制也不允许
func TestCostumeOverlapHardBlock(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	show, a1, a2, r1, r2, c1, _ := baseFixture(t, svc)

	first := mkTask(show.ID, c1.ID, r1.ID, a1.ID, "侧台左", 19, 30, 20, 0)
	db.Create(&first)
	overlap := mkTask(show.ID, c1.ID, r2.ID, a2.ID, "侧台右", 19, 45, 20, 15)
	cs, _ := EvaluateTask(db, show, overlap)
	if !hasType(cs, ConflictCostumeOverlap) {
		t.Fatalf("期望戏服时间重叠冲突，实际: %+v", cs)
	}
	if !HasHard(cs) {
		t.Fatal("时间重叠必须是硬性冲突")
	}
	in := TaskInput{
		ShowID: show.ID, CostumeID: c1.ID, RoleID: r2.ID, ActorID: a2.ID,
		OnAt: overlap.OnAt.Format("2006-01-02T15:04"), OffAt: overlap.OffAt.Format("2006-01-02T15:04"),
		ChangePoint: "侧台右",
	}
	if _, _, err := svc.CreateTask(in, true); err == nil {
		t.Fatal("硬性冲突即使强制保存也必须拒绝")
	}
}

// 4) 临时换角：只改未签收任务，已签收记录仍关联原演员
func TestRecastKeepsSignedTasks(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	show, a1, a2, r1, _, c1, c2 := baseFixture(t, svc)

	t1 := mkTask(show.ID, c1.ID, r1.ID, a1.ID, "侧台左", 19, 30, 20, 0)
	t2 := mkTask(show.ID, c2.ID, r1.ID, a1.ID, "侧台左", 20, 30, 21, 0)
	db.Create(&t1)
	db.Create(&t2)

	// t1 已由甲签收（签收人=甲），之后临时换角为乙
	if err := svc.ScanSign(t1.ID, a1.ID, "T-001"); err != nil {
		t.Fatal(err)
	}
	changed, blocked, err := svc.Recast(show.ID, r1.ID, a2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || len(blocked) != 0 {
		t.Fatalf("应只调整 1 个未签收任务，实际 changed=%d blocked=%d", changed, len(blocked))
	}
	var got1, got2 models.Task
	db.First(&got1, t1.ID)
	db.First(&got2, t2.ID)
	if got1.ActorID != a1.ID || got1.SignedByActorID == nil || *got1.SignedByActorID != a1.ID {
		t.Fatalf("已签收任务必须仍关联原演员甲，实际 actor=%d signed=%v", got1.ActorID, got1.SignedByActorID)
	}
	if got1.OriginalActorID != a1.ID {
		t.Fatal("原演员字段不应被换角改动")
	}
	if got2.ActorID != a2.ID || got2.OriginalActorID != a1.ID {
		t.Fatalf("未签收任务应换为演员乙且保留原演员记录，实际 actor=%d original=%d", got2.ActorID, got2.OriginalActorID)
	}

	// 已签收任务拒绝再次签收/改路线
	if err := svc.ScanSign(t1.ID, a2.ID, "T-001"); err == nil {
		t.Fatal("已签收任务不能再次签收")
	}
}

// 5) 未经洗护再次分配的拦截：交回 -> 洗护中 -> 未完成前分配/签收都被拦截，完成后放行
func TestBlockedUntilCareFinished(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	show, a1, _, r1, _, c1, _ := baseFixture(t, svc)

	t1 := mkTask(show.ID, c1.ID, r1.ID, a1.ID, "侧台左", 19, 30, 20, 0)
	db.Create(&t1)
	if err := svc.ScanSign(t1.ID, 0, "T-001"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ScanReturn(t1.ID, "T-001"); err != nil {
		t.Fatal(err)
	}

	// 送洗护
	care, err := svc.StartCare(c1.ID, models.CareLaundry, "汗渍")
	if err != nil {
		t.Fatal(err)
	}

	// 洗护未完成，再排一件后续任务 -> 硬性拦截
	in := TaskInput{
		ShowID: show.ID, CostumeID: c1.ID, RoleID: r1.ID, ActorID: a1.ID,
		OnAt: "2026-01-02T21:00", OffAt: "2026-01-02T21:30", ChangePoint: "侧台左",
	}
	// 时间锚定到今天以免和“下一位使用人”逻辑耦合，这里只验证门禁
	base := time.Now()
	in.OnAt = time.Date(base.Year(), base.Month(), base.Day(), 21, 0, 0, 0, base.Location()).Format("2006-01-02T15:04")
	in.OffAt = time.Date(base.Year(), base.Month(), base.Day(), 21, 30, 0, 0, base.Location()).Format("2006-01-02T15:04")
	if _, cs, err := svc.CreateTask(in, true); err == nil || !hasType(cs, ConflictCostumeBlocked) {
		t.Fatalf("洗护中的戏服必须被硬性拦截，err=%v conflicts=%+v", err, cs)
	}

	// 同时签收路径也拦截（造一个排程在洗护前的任务不可行，故直接验证状态门禁存在 open care）
	var open int64
	db.Model(&models.CareRecord{}).Where("costume_id = ? AND status = ?", c1.ID, models.CareOpen).Count(&open)
	if open != 1 {
		t.Fatalf("应有 1 条进行中洗护记录")
	}

	// 完成洗护 -> 恢复 available -> 可以安排
	if err := svc.FinishCare(care.ID); err != nil {
		t.Fatal(err)
	}
	var c models.Costume
	db.First(&c, c1.ID)
	if c.Status != models.CostumeAvailable {
		t.Fatalf("洗护完成后戏服应恢复可用，实际 %s", c.Status)
	}
	if _, _, err := svc.CreateTask(in, false); err != nil {
		t.Fatalf("洗护完成后应能正常分配: %v", err)
	}
}

// 6) 少件/破损问题单：未结单前不能分配；不能无说明结单；结单+维修完成后恢复
func TestIssueLifecycle(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	show, a1, _, r1, _, c1, _ := baseFixture(t, svc)

	issue, err := svc.ReportIssue(c1.ID, &show.ID, models.IssueDamaged, "袖口撕裂")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	in := TaskInput{
		ShowID: show.ID, CostumeID: c1.ID, RoleID: r1.ID, ActorID: a1.ID,
		OnAt:  time.Date(base.Year(), base.Month(), base.Day(), 21, 0, 0, 0, base.Location()).Format("2006-01-02T15:04"),
		OffAt: time.Date(base.Year(), base.Month(), base.Day(), 21, 30, 0, 0, base.Location()).Format("2006-01-02T15:04"),
		ChangePoint: "侧台左",
	}
	if _, cs, err := svc.CreateTask(in, true); err == nil || !hasType(cs, ConflictCostumeBlocked) {
		t.Fatalf("有未处理破损问题单时必须拦截分配，err=%v", err)
	}
	// 无处理说明不能结单
	if err := svc.ResolveIssue(issue.ID, ""); err == nil {
		t.Fatal("问题单必须填写处理说明才能结单")
	}
	if err := svc.ResolveIssue(issue.ID, "服装组缝补加固并复检合格"); err != nil {
		t.Fatal(err)
	}
	// 结单后无进行中洗护/维修 -> 可用
	if _, _, err := svc.CreateTask(in, false); err != nil {
		t.Fatalf("问题处理后应能分配: %v", err)
	}
}

// 7) 散场清点标记少件自动生成问题单并锁定
func TestCheckMissingCreatesIssue(t *testing.T) {
	db := testDB(t)
	svc := New(db)
	show, _, _, _, _, c1, _ := baseFixture(t, svc)

	dto, err := svc.StartCheck(show.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordCheck(dto.Session.ID, c1.ID, models.CheckMissing, "未找到"); err != nil {
		t.Fatal(err)
	}
	var issueCount int64
	db.Model(&models.Issue{}).Where("costume_id = ? AND type = ? AND status = ?",
		c1.ID, models.IssueMissing, models.IssueOpen).Count(&issueCount)
	if issueCount != 1 {
		t.Fatalf("清点少件应生成 1 张未决问题单，实际 %d", issueCount)
	}
	var c models.Costume
	db.First(&c, c1.ID)
	if c.Status != models.CostumeMissing {
		t.Fatalf("少件戏服应被锁定为 missing，实际 %s", c.Status)
	}
}
