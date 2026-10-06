package service

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"theater-costumes/internal/models"
)

type Service struct{ DB *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{DB: db} }

// ---------- 基础 CRUD ----------

type TaskInput struct {
	ShowID      uint   `json:"showId"`
	CostumeID   uint   `json:"costumeId"`
	RoleID      uint   `json:"roleId"`
	ActorID     uint   `json:"actorId"`
	SceneID     uint   `json:"sceneId"`
	Seq         int    `json:"seq"`
	OnAt        string `json:"onAt"`
	OffAt       string `json:"offAt"`
	ChangePoint string `json:"changePoint"`
}

func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间: %s", s)
}

func (s *Service) ValidateTaskInput(in TaskInput) (models.Task, models.Show, error) {
	var t models.Task
	var show models.Show
	if err := s.DB.First(&show, in.ShowID).Error; err != nil {
		return t, show, errors.New("演出不存在")
	}
	on, err := parseTime(in.OnAt)
	if err != nil {
		return t, show, err
	}
	off, err := parseTime(in.OffAt)
	if err != nil {
		return t, show, err
	}
	if !off.After(on) {
		return t, show, errors.New("下场时间必须晚于上场时间")
	}
	var c models.Costume
	if err := s.DB.First(&c, in.CostumeID).Error; err != nil {
		return t, show, errors.New("戏服不存在")
	}
	t = models.Task{
		ShowID: in.ShowID, CostumeID: in.CostumeID, RoleID: in.RoleID,
		ActorID: in.ActorID, SceneID: in.SceneID, Seq: in.Seq,
		OnAt: on, OffAt: off, ChangePoint: in.ChangePoint,
		Status: models.TaskPending, OriginalActorID: in.ActorID,
	}
	return t, show, nil
}

// CheckTask 只做冲突预检，不落库
func (s *Service) CheckTask(in TaskInput) ([]Conflict, error) {
	t, show, err := s.ValidateTaskInput(in)
	if err != nil {
		return nil, err
	}
	// 预检时用临时 id，库里的已保存任务都参与比较
	return EvaluateTask(s.DB, show, t)
}

// CreateTask 排程换装任务。硬性冲突直接拒绝；
// 软性冲突（换装紧张/周转紧张）默认拒绝，force=true 时允许并把冲突写入 ConflictNote
func (s *Service) CreateTask(in TaskInput, force bool) (*models.Task, []Conflict, error) {
	t, show, err := s.ValidateTaskInput(in)
	if err != nil {
		return nil, nil, err
	}
	cs, err := EvaluateTask(s.DB, show, t)
	if err != nil {
		return nil, nil, err
	}
	if HasHard(cs) {
		return nil, cs, errors.New("存在硬性冲突，无法安排（请先解决洗护/问题单或时间重叠）")
	}
	if len(cs) > 0 && !force {
		return nil, cs, errors.New("存在换装/周转冲突，确认仍要安排可强制保存")
	}
	if force {
		t.ConflictNote = joinMessages(cs)
	}
	if err := s.DB.Create(&t).Error; err != nil {
		return nil, nil, err
	}
	return &t, cs, nil
}

// UpdateTask 修改未执行任务；已签收/已交回的任务只能改备注
func (s *Service) UpdateTask(id uint, in TaskInput, force bool) (*models.Task, []Conflict, error) {
	var t models.Task
	if err := s.DB.First(&t, id).Error; err != nil {
		return nil, nil, errors.New("任务不存在")
	}
	if t.Status != models.TaskPending {
		return nil, nil, fmt.Errorf("任务已%s，不能再修改路线", taskStatusText(t.Status))
	}
	nt, show, err := s.ValidateTaskInput(in)
	if err != nil {
		return nil, nil, err
	}
	cs, err := EvaluateTask(s.DB, show, nt)
	if err != nil {
		return nil, nil, err
	}
	if HasHard(cs) {
		return nil, cs, errors.New("存在硬性冲突，无法保存")
	}
	if len(cs) > 0 && !force {
		return nil, cs, errors.New("存在换装/周转冲突，确认仍要保存可强制保存")
	}
	t.ShowID = nt.ShowID
	t.CostumeID = nt.CostumeID
	t.RoleID = nt.RoleID
	t.ActorID = nt.ActorID
	t.SceneID = nt.SceneID
	t.Seq = nt.Seq
	t.OnAt = nt.OnAt
	t.OffAt = nt.OffAt
	t.ChangePoint = nt.ChangePoint
	t.ConflictNote = ""
	if force {
		t.ConflictNote = joinMessages(cs)
	}
	if err := s.DB.Save(&t).Error; err != nil {
		return nil, nil, err
	}
	return &t, cs, nil
}

// Recast 临时换角：只调整尚未签收（pending）的任务；
// 已签收/已交回的记录保留原演员，签收记录关联实际签收人
func (s *Service) Recast(showID, roleID, newActorID uint) (int, []Conflict, error) {
	var newActor models.Actor
	if err := s.DB.First(&newActor, newActorID).Error; err != nil {
		return 0, nil, errors.New("新演员不存在")
	}
	var show models.Show
	if err := s.DB.First(&show, showID).Error; err != nil {
		return 0, nil, errors.New("演出不存在")
	}
	var tasks []models.Task
	if err := s.DB.Where("show_id = ? AND role_id = ? AND status = ?",
		showID, roleID, models.TaskPending).Find(&tasks).Error; err != nil {
		return 0, nil, err
	}
	changed := 0
	var blocked []Conflict
	for i := range tasks {
		t := tasks[i]
		oldActor := t.ActorID
		t.ActorID = newActorID
		cs, err := EvaluateTask(s.DB, show, t)
		if err != nil {
			return changed, nil, err
		}
		// 换角后产生冲突的任务跳过并收集，其余任务照常换角
		if len(cs) > 0 {
			blocked = append(blocked, cs...)
			continue
		}
		if err := s.DB.Model(&models.Task{}).Where("id = ?", t.ID).
			Update("actor_id", newActorID).Error; err != nil {
			return changed, nil, err
		}
		_ = oldActor
		changed++
	}
	// 已签收/已交回任务的 OriginalActorId / SignedByActorId 原样保留（查询层体现）
	return changed, blocked, nil
}

// ---------- 模拟扫码 ----------

// ScanSign 演员扫码签收戏服（任务开始执行，此后路线与演员冻结）
func (s *Service) ScanSign(taskID, actorID uint, code string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var task models.Task
		if err := tx.First(&task, taskID).Error; err != nil {
			return errors.New("任务不存在")
		}
		if task.Status != models.TaskPending {
			return fmt.Errorf("任务已%s，不能签收", taskStatusText(task.Status))
		}
		var costume models.Costume
		if err := tx.First(&costume, task.CostumeID).Error; err != nil {
			return err
		}
		if costume.Code != code {
			return fmt.Errorf("扫码实物编号 %s 与任务要求的 %s 不符", code, costume.Code)
		}
		// 签收前最后一次门禁：防止排程后戏服被送去洗护/报损
		var openCare int64
		tx.Model(&models.CareRecord{}).Where("costume_id = ? AND status = ?", costume.ID, models.CareOpen).Count(&openCare)
		var openIssue int64
		tx.Model(&models.Issue{}).Where("costume_id = ? AND status = ?", costume.ID, models.IssueOpen).Count(&openIssue)
		if openCare > 0 || openIssue > 0 {
			return errors.New("该戏服存在未完成的洗护/维修或问题单，不能签收")
		}
		now := time.Now()
		a := actorID
		if a == 0 {
			a = task.ActorID
		}
		if err := tx.Model(&task).Updates(map[string]any{
			"status": models.TaskSigned, "signed_by_actor_id": a, "signed_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&costume).Updates(map[string]any{
			"status": models.CostumeInUse, "holder_id": a,
			"location": task.ChangePoint,
		}).Error
	})
}

// ScanReturn 服装师扫码收回戏服（任务完成，衣服离开演员）
func (s *Service) ScanReturn(taskID uint, code string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var task models.Task
		if err := tx.First(&task, taskID).Error; err != nil {
			return errors.New("任务不存在")
		}
		if task.Status != models.TaskSigned {
			return fmt.Errorf("任务当前状态为「%s」，不能交回", taskStatusText(task.Status))
		}
		var costume models.Costume
		if err := tx.First(&costume, task.CostumeID).Error; err != nil {
			return err
		}
		if costume.Code != code {
			return fmt.Errorf("扫码实物编号 %s 与任务要求的 %s 不符", code, costume.Code)
		}
		now := time.Now()
		if err := tx.Model(&task).Updates(map[string]any{
			"status": models.TaskReturned, "returned_at": now,
		}).Error; err != nil {
			return err
		}
		// 戏服回到服装间，恢复可排程状态（能否再次分配仍受清洁间隔/后续洗护记录约束）
		return tx.Model(&costume).Updates(map[string]any{
			"status": models.CostumeAvailable, "location": "服装间", "holder_id": nil,
		}).Error
	})
}

// ---------- 洗护 / 维修 ----------

func (s *Service) StartCare(costumeID uint, kind, note string) (*models.CareRecord, error) {
	if kind != models.CareLaundry && kind != models.CareRepair {
		return nil, errors.New("类型必须是 laundry 或 repair")
	}
	var c models.Costume
	if err := s.DB.First(&c, costumeID).Error; err != nil {
		return nil, errors.New("戏服不存在")
	}
	var openCare int64
	s.DB.Model(&models.CareRecord{}).Where("costume_id = ? AND status = ?", costumeID, models.CareOpen).Count(&openCare)
	if openCare > 0 {
		return nil, errors.New("该戏服已有进行中的洗护/维修")
	}
	var openIssue int64
	s.DB.Model(&models.Issue{}).Where("costume_id = ? AND status = ?", costumeID, models.IssueOpen).Count(&openIssue)
	if openIssue > 0 {
		return nil, errors.New("该戏服有未处理的少件/破损问题单，需先在问题单中处理")
	}
	if c.HolderID != nil || c.Status == models.CostumeInUse {
		return nil, errors.New("戏服仍在演员手中（已签收未交回），不能送去洗护/维修")
	}
	rec := models.CareRecord{
		CostumeID: costumeID, Type: kind, Status: models.CareOpen,
		Note: note, StartedAt: time.Now(),
	}
	if err := s.DB.Create(&rec).Error; err != nil {
		return nil, err
	}
	status := models.CostumeLaundering
	if kind == models.CareRepair {
		status = models.CostumeRepairing
	}
	if err := s.DB.Model(&c).Update("status", status).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// FinishCare 洗护/维修完成后戏服才恢复 available
func (s *Service) FinishCare(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var rec models.CareRecord
		if err := tx.First(&rec, id).Error; err != nil {
			return errors.New("洗护/维修记录不存在")
		}
		if rec.Status == models.CareDone {
			return errors.New("该记录已完成")
		}
		var openIssue int64
		tx.Model(&models.Issue{}).Where("costume_id = ? AND status = ?", rec.CostumeID, models.IssueOpen).Count(&openIssue)
		now := time.Now()
		if err := tx.Model(&rec).Updates(map[string]any{
			"status": models.CareDone, "ended_at": now,
		}).Error; err != nil {
			return err
		}
		updates := map[string]any{"status": models.CostumeAvailable, "location": "服装间"}
		if openIssue > 0 {
			// 问题未处理不能恢复可用
			updates["status"] = models.CostumeDamaged
		}
		return tx.Model(&models.Costume{}).Where("id = ?", rec.CostumeID).Updates(updates).Error
	})
}

// ---------- 问题单（少件 / 破损） ----------

func (s *Service) ReportIssue(costumeID uint, showID *uint, kind, note string) (*models.Issue, error) {
	if kind != models.IssueMissing && kind != models.IssueDamaged {
		return nil, errors.New("类型必须是 missing 或 damaged")
	}
	var c models.Costume
	if err := s.DB.First(&c, costumeID).Error; err != nil {
		return nil, errors.New("戏服不存在")
	}
	issue := models.Issue{
		CostumeID: costumeID, ShowID: showID, Type: kind,
		Status: models.IssueOpen, Note: note, ReportedAt: time.Now(),
	}
	if err := s.DB.Create(&issue).Error; err != nil {
		return nil, err
	}
	status := models.CostumeMissing
	if kind == models.IssueDamaged {
		status = models.CostumeDamaged
	}
	s.DB.Model(&c).Update("status", status)
	return &issue, nil
}

// ResolveIssue 处理问题单（找回/修补等），说明必须填写；不允许用“补库存数量”消除
func (s *Service) ResolveIssue(id uint, resolution string) error {
	if resolution == "" {
		return errors.New("必须填写处理说明（找回/修复经过），不能靠补数量消除问题")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var issue models.Issue
		if err := tx.First(&issue, id).Error; err != nil {
			return errors.New("问题单不存在")
		}
		if issue.Status == models.IssueResolved {
			return errors.New("问题单已处理")
		}
		now := time.Now()
		if err := tx.Model(&issue).Updates(map[string]any{
			"status": models.IssueResolved, "resolution": resolution, "resolved_at": now,
		}).Error; err != nil {
			return err
		}
		// 破损通常需维修；若没有进行中的维修记录，回到待处理洗护区而不是直接可用，
		// 这里恢复“可用”的前提是没有 open 的洗护/维修；破损问题若需维修应再走维修流程。
		var openCare int64
		tx.Model(&models.CareRecord{}).Where("costume_id = ? AND status = ?", issue.CostumeID, models.CareOpen).Count(&openCare)
		status := models.CostumeAvailable
		if openCare > 0 {
			status = models.CostumeRepairing
		}
		return tx.Model(&models.Costume{}).Where("id = ?", issue.CostumeID).
			Updates(map[string]any{"status": status, "location": "服装间"}).Error
	})
}

// ---------- 散场清点 ----------

func (s *Service) StartCheck(showID uint) (*CheckSessionDTO, error) {
	var show models.Show
	if err := s.DB.First(&show, showID).Error; err != nil {
		return nil, errors.New("演出不存在")
	}
	var costumes []models.Costume
	if err := s.DB.Find(&costumes).Error; err != nil {
		return nil, err
	}
	session := models.CheckSession{ShowID: showID, CreatedAt: time.Now()}
	if err := s.DB.Create(&session).Error; err != nil {
		return nil, err
	}
	for _, c := range costumes {
		item := models.CheckItem{SessionID: session.ID, CostumeID: c.ID, State: models.CheckPresent, CreatedAt: time.Now()}
		s.DB.Create(&item)
	}
	return s.GetCheck(session.ID)
}

type CheckSessionDTO struct {
	Session models.CheckSession  `json:"session"`
	Items   []CheckItemView      `json:"items"`
}

type CheckItemView struct {
	models.CheckItem
	CostumeCode string `json:"costumeCode"`
	CostumeName string `json:"costumeName"`
}

func (s *Service) GetCheck(sessionID uint) (*CheckSessionDTO, error) {
	var session models.CheckSession
	if err := s.DB.First(&session, sessionID).Error; err != nil {
		return nil, errors.New("清点单不存在")
	}
	var items []models.CheckItem
	if err := s.DB.Where("session_id = ?", sessionID).Find(&items).Error; err != nil {
		return nil, err
	}
	views := make([]CheckItemView, 0, len(items))
	for _, it := range items {
		var c models.Costume
		s.DB.First(&c, it.CostumeID)
		views = append(views, CheckItemView{CheckItem: it, CostumeCode: c.Code, CostumeName: c.Name})
	}
	return &CheckSessionDTO{Session: session, Items: views}, nil
}

// RecordCheck 逐件清点：present / missing / damaged；
// missing 与 damaged 自动生成未决问题单并锁定戏服，不允许只改数量
func (s *Service) RecordCheck(sessionID, costumeID uint, state, note string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var item models.CheckItem
		if err := tx.Where("session_id = ? AND costume_id = ?", sessionID, costumeID).First(&item).Error; err != nil {
			return errors.New("清点项不存在")
		}
		var session models.CheckSession
		if err := tx.First(&session, sessionID).Error; err != nil {
			return err
		}
		if session.Closed {
			return errors.New("清点单已关闭")
		}
		if state != models.CheckPresent && state != models.CheckMissing && state != models.CheckDamaged {
			return errors.New("清点状态非法")
		}
		if err := tx.Model(&item).Updates(map[string]any{"state": state, "note": note}).Error; err != nil {
			return err
		}
		var c models.Costume
		if err := tx.First(&c, costumeID).Error; err != nil {
			return err
		}
		switch state {
		case models.CheckMissing:
			if err := tx.Model(&c).Update("status", models.CostumeMissing).Error; err != nil {
				return err
			}
			issue := models.Issue{CostumeID: costumeID, ShowID: &session.ShowID,
				Type: models.IssueMissing, Status: models.IssueOpen,
				Note: noteOr(note, "散场清点发现少件"), ReportedAt: time.Now()}
			return tx.Create(&issue).Error
		case models.CheckDamaged:
			if err := tx.Model(&c).Update("status", models.CostumeDamaged).Error; err != nil {
				return err
			}
			issue := models.Issue{CostumeID: costumeID, ShowID: &session.ShowID,
				Type: models.IssueDamaged, Status: models.IssueOpen,
				Note: noteOr(note, "散场清点发现破损"), ReportedAt: time.Now()}
			return tx.Create(&issue).Error
		case models.CheckPresent:
			// 在场但之前可能标过异常：若仍有未决问题单，不恢复状态
			var openIssue int64
			tx.Model(&models.Issue{}).Where("costume_id = ? AND status = ?", costumeID, models.IssueOpen).Count(&openIssue)
			if openIssue == 0 && c.HolderID == nil && c.Status != models.CostumeLaundering && c.Status != models.CostumeRepairing {
				return tx.Model(&c).Update("status", models.CostumeAvailable).Error
			}
		}
		return nil
	})
}

func noteOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func joinMessages(cs []Conflict) string {
	out := ""
	for i, c := range cs {
		if i > 0 {
			out += "；"
		}
		out += c.Message
	}
	return out
}

func taskStatusText(s string) string {
	switch s {
	case models.TaskPending:
		return "待签收"
	case models.TaskSigned:
		return "签收"
	case models.TaskReturned:
		return "交回"
	case models.TaskCancelled:
		return "取消"
	}
	return s
}
