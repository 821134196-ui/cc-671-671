package service

import (
	"time"

	"gorm.io/gorm"
	"theater-costumes/internal/models"
)

// ResetAndSeed 清空并写入演示数据（剧目《夜宴》），
// 任务时间锚定到今天 19:30–22:00，便于观察“下一位使用人”。
func (s *Service) ResetAndSeed() error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for _, m := range models.AllModels() {
			if err := tx.Where("1=1").Delete(m).Error; err != nil {
				return err
			}
		}

		show := models.Show{
			Name: "《夜宴》", PerformanceDate: time.Now().Format("2006-01-02"),
			TransferSeconds: 180,  // 换装点之间转场 3 分钟
			CleaningSeconds: 900,  // 不同角色共用需清洁间隔 15 分钟
			Status:          "planned",
		}
		tx.Create(&show)

		roles := []models.Role{
			{ShowID: show.ID, Name: "王子"},
			{ShowID: show.ID, Name: "公主"},
			{ShowID: show.ID, Name: "使者"},
			{ShowID: show.ID, Name: "侍卫长"},
		}
		for i := range roles {
			tx.Create(&roles[i])
		}
		actors := []models.Actor{
			{Name: "林岚"}, {Name: "苏晴"}, {Name: "赵铎"}, {Name: "陈骁"},
		}
		for i := range actors {
			tx.Create(&actors[i])
		}
		scenes := []models.Scene{
			{ShowID: show.ID, Name: "第一幕·宫门", Seq: 1},
			{ShowID: show.ID, Name: "第二幕·宴厅", Seq: 2},
			{ShowID: show.ID, Name: "第三幕·回廊", Seq: 3},
			{ShowID: show.ID, Name: "第四幕·真相", Seq: 4},
		}
		for i := range scenes {
			tx.Create(&scenes[i])
		}
		costumes := []models.Costume{
			{Code: "C-001", Name: "王子礼服（黑金）", ChangeSeconds: 300, Status: models.CostumeAvailable, Location: "服装间"},
			{Code: "C-002", Name: "公主长裙（绛红）", ChangeSeconds: 420, Status: models.CostumeAvailable, Location: "服装间"},
			{Code: "C-003", Name: "使者斗篷（灰）", ChangeSeconds: 180, Status: models.CostumeAvailable, Location: "服装间"},
			{Code: "C-004", Name: "侍卫铠甲", ChangeSeconds: 360, Status: models.CostumeAvailable, Location: "服装间"},
			{Code: "C-005", Name: "夜行衣（共用）", ChangeSeconds: 240, Status: models.CostumeAvailable, Location: "服装间"},
			{Code: "C-006", Name: "王子常服", ChangeSeconds: 240, Status: models.CostumeAvailable, Location: "服装间"},
		}
		for i := range costumes {
			tx.Create(&costumes[i])
		}

		base := time.Now()
		at := func(h, m int) time.Time {
			t := time.Date(base.Year(), base.Month(), base.Day(), h, m, 0, 0, base.Location())
			return t
		}
		tasks := []models.Task{
			// 林岚：王子礼服 宫门 -> 很快又要在宴厅穿王子常服（换装紧张的演示靠手动安排；
			// 这里先排正常路线）
			{ShowID: show.ID, CostumeID: costumes[0].ID, RoleID: roles[0].ID, ActorID: actors[0].ID,
				SceneID: scenes[0].ID, Seq: 1, OnAt: at(19, 30), OffAt: at(20, 5),
				ChangePoint: "侧台左", OriginalActorID: actors[0].ID, Status: models.TaskPending},
			{ShowID: show.ID, CostumeID: costumes[5].ID, RoleID: roles[0].ID, ActorID: actors[0].ID,
				SceneID: scenes[2].ID, Seq: 3, OnAt: at(20, 40), OffAt: at(21, 20),
				ChangePoint: "侧台右", OriginalActorID: actors[0].ID, Status: models.TaskPending},
			// 苏晴：公主
			{ShowID: show.ID, CostumeID: costumes[1].ID, RoleID: roles[1].ID, ActorID: actors[1].ID,
				SceneID: scenes[1].ID, Seq: 2, OnAt: at(20, 10), OffAt: at(20, 35),
				ChangePoint: "侧台左", OriginalActorID: actors[1].ID, Status: models.TaskPending},
			// 赵铎：使者斗篷 第二幕
			{ShowID: show.ID, CostumeID: costumes[2].ID, RoleID: roles[2].ID, ActorID: actors[2].ID,
				SceneID: scenes[1].ID, Seq: 4, OnAt: at(20, 12), OffAt: at(20, 30),
				ChangePoint: "侧台右", OriginalActorID: actors[2].ID, Status: models.TaskPending},
			// 陈骁：侍卫铠甲
			{ShowID: show.ID, CostumeID: costumes[3].ID, RoleID: roles[3].ID, ActorID: actors[3].ID,
				SceneID: scenes[0].ID, Seq: 5, OnAt: at(19, 30), OffAt: at(21, 0),
				ChangePoint: "侧台右", OriginalActorID: actors[3].ID, Status: models.TaskPending},
			// 夜行衣：赵铎第四幕再用（与林岚第三幕衔接，15分钟清洁间隔——边界可演示冲突）
			{ShowID: show.ID, CostumeID: costumes[4].ID, RoleID: roles[2].ID, ActorID: actors[2].ID,
				SceneID: scenes[3].ID, Seq: 6, OnAt: at(21, 30), OffAt: at(21, 50),
				ChangePoint: "侧台左", OriginalActorID: actors[2].ID, Status: models.TaskPending},
		}
		for i := range tasks {
			tx.Create(&tasks[i])
		}
		return nil
	})
}

var _ = gorm.ErrRecordNotFound
