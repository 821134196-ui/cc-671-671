package store

import (
	"time"

	"theater-costume/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open 打开/创建 SQLite 数据库并执行表结构迁移。
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
		NowFunc: func() time.Time {
			return time.Now().Local()
		},
	})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(
		&models.Actor{},
		&models.Costume{},
		&models.Performance{},
		&models.Assignment{},
		&models.CareRecord{},
		&models.Incident{},
		&models.InventoryCheck{},
		&models.RecastLog{},
	); err != nil {
		return nil, err
	}
	return db, nil
}

// Seed 在空库中写入演示数据，并刻意制造三类问题，方便直接在页面看到冲突：
//  1. 同一件衣服两场之间转场/清洁时间不足（C001）；
//  2. 同一位演员连续上场来不及换装（A001 在 scene 1→2）；
//  3. 一件散场未洗护的衣服被排进新场（C003，由演示脚本/接口演示拦截）。
func Seed(db *gorm.DB) error {
	var cnt int64
	db.Model(&models.Costume{}).Count(&cnt)
	if cnt > 0 {
		return nil
	}

	base := time.Date(2026, 10, 10, 19, 30, 0, 0, time.Local)

	actors := []models.Actor{
		{Name: "林岚", Role: "女主角 艾琳", Phone: "13800000001"},
		{Name: "周岩", Role: "男主角 罗杰", Phone: "13800000002"},
		{Name: "苏晓", Role: "女仆 梅", Phone: "13800000003"},
		{Name: "陈戈", Role: "管家 霍普", Phone: "13800000004"},
	}
	if err := db.Create(&actors).Error; err != nil {
		return err
	}

	cleaned := base.Add(-48 * time.Hour)
	costumes := []models.Costume{
		{Code: "C001", Name: "酒红色晚礼服", Category: "礼服", Status: models.CostumeAvailable,
			ChangeSeconds: 180, CleanSeconds: 1800, LastCleanedAt: &cleaned, CurrentLocation: "主换装间 A 架"},
		{Code: "C002", Name: "黑色燕尾服", Category: "礼服", Status: models.CostumeAvailable,
			ChangeSeconds: 240, CleanSeconds: 1800, LastCleanedAt: &cleaned, CurrentLocation: "主换装间 B 架"},
		{Code: "C003", Name: "白色女仆裙", Category: "便服", Status: models.CostumeReturned,
			ChangeSeconds: 120, CleanSeconds: 3600, CurrentLocation: "洗衣房待洗篮"},
		{Code: "C004", Name: "深灰管家制服", Category: "制服", Status: models.CostumeAvailable,
			ChangeSeconds: 150, CleanSeconds: 1800, LastCleanedAt: &cleaned, CurrentLocation: "主换装间 C 架"},
		{Code: "C005", Name: "黑色暗纹斗篷", Category: "外套", Status: models.CostumeAvailable,
			ChangeSeconds: 300, CleanSeconds: 1800, LastCleanedAt: &cleaned, CurrentLocation: "主换装间 B 架"},
	}
	if err := db.Create(&costumes).Error; err != nil {
		return err
	}

	perf := models.Performance{
		Name: "《夜色庄园》首演夜", Venue: "城市剧院·主舞台",
		ShowTime: base, Status: "scheduled",
	}
	if err := db.Create(&perf).Error; err != nil {
		return err
	}

	mk := func(cid, aid uint, seq int, scene string, handover, needby, ret time.Time, point string) models.Assignment {
		return models.Assignment{
			PerformanceID: perf.ID, CostumeID: cid, ActorID: aid, PlannedActorID: aid,
			Sequence: seq, Scene: scene,
			HandoverAt: handover, NeedByAt: needby, ReturnAt: ret,
			ChangePoint: point, Status: models.TaskPending,
		}
	}

	assignments := []models.Assignment{
		// ① 林岚 第一幕穿 C001：19:40 签收，19:45 上场，20:05 交回。
		mk(costumes[0].ID, actors[0].ID, 1, "第一幕",
			base.Add(10*time.Minute), base.Add(15*time.Minute), base.Add(35*time.Minute), "上场门换装点 1"),
		// ② 周岩 第一幕 C002，正常路线。
		mk(costumes[1].ID, actors[1].ID, 2, "第一幕",
			base.Add(8*time.Minute), base.Add(15*time.Minute), base.Add(35*time.Minute), "上场门换装点 2"),
		// ③ 林岚 第二幕换 C005 斗篷：20:05 下场，20:06 签收，20:09 必须上场，
		//    斗篷换装需 300 秒 → 演员赶场/来不及换装冲突。
		mk(costumes[4].ID, actors[0].ID, 3, "第二幕",
			base.Add(36*time.Minute), base.Add(39*time.Minute), base.Add(70*time.Minute), "上场门换装点 1"),
		// ④ C001 转给苏晓：20:05 林岚交回，20:20 苏晓就要用，
		//    间隔 15 分钟 < 清洁要求 30 分钟，且不同演员共用 → 清洁间隔冲突。
		mk(costumes[0].ID, actors[2].ID, 4, "第二幕",
			base.Add(50*time.Minute), base.Add(55*time.Minute), base.Add(80*time.Minute), "侧廊换装点"),
		// ⑤ 陈戈 第三幕 C004，正常路线。
		mk(costumes[3].ID, actors[3].ID, 5, "第三幕",
			base.Add(82*time.Minute), base.Add(90*time.Minute), base.Add(120*time.Minute), "上场门换装点 2"),
		// ⑥ 苏晓 第三幕 C003：C003 目前是“已交回待洗护”状态（洗衣房待洗篮），
		//    未经洗护再次分配 → 场次页直接报阻断冲突，建单接口会硬拦截。
		mk(costumes[2].ID, actors[2].ID, 6, "第三幕",
			base.Add(85*time.Minute), base.Add(90*time.Minute), base.Add(105*time.Minute), "侧廊换装点"),
	}
	if err := db.Create(&assignments).Error; err != nil {
		return err
	}
	return nil
}
