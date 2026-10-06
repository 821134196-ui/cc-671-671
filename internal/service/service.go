package service

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"theater-costume/internal/models"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("记录不存在")

// Service 承载全部业务规则。服装状态只能通过签收/交回/洗护/清点/事件
// 这些业务动作迁移，不提供“直接改状态/补数量”的捷径。
type Service struct {
	DB *gorm.DB
}

func New(db *gorm.DB) *Service { return &Service{DB: db} }

// ----------------------------- 基础资料 -----------------------------

func (s *Service) ListActors() ([]models.Actor, error) {
	var out []models.Actor
	err := s.DB.Order("id").Find(&out).Error
	return out, err
}

func (s *Service) CreateActor(a *models.Actor) error {
	return s.DB.Create(a).Error
}

func (s *Service) ListCostumes() ([]models.Costume, error) {
	var out []models.Costume
	err := s.DB.Order("code").Find(&out).Error
	return out, err
}

func (s *Service) GetCostume(id uint) (*models.Costume, error) {
	var c models.Costume
	if err := s.DB.First(&c, id).Error; err != nil {
		return nil, ErrNotFound
	}
	return &c, nil
}

// CreateCostume 登记一件实物服装。新衣服默认 available，位置为主换装间。
func (s *Service) CreateCostume(c *models.Costume) error {
	if c.Code == "" {
		return errors.New("实物编号不能为空")
	}
	if c.ChangeSeconds <= 0 {
		c.ChangeSeconds = 120
	}
	if c.CleanSeconds <= 0 {
		c.CleanSeconds = 1800
	}
	c.Status = models.CostumeAvailable
	if c.CurrentLocation == "" {
		c.CurrentLocation = "主换装间"
	}
	now := time.Now()
	c.LastCleanedAt = &now
	return s.DB.Create(c).Error
}

func (s *Service) ListPerformances() ([]models.Performance, error) {
	var out []models.Performance
	err := s.DB.Order("show_time desc").Find(&out).Error
	return out, err
}

func (s *Service) CreatePerformance(p *models.Performance) error {
	if p.Status == "" {
		p.Status = "scheduled"
	}
	return s.DB.Create(p).Error
}

// ----------------------------- 换装建单 -----------------------------

type AssignmentInput struct {
	PerformanceID uint   `json:"performance_id"`
	CostumeID     uint   `json:"costume_id"`
	ActorID       uint   `json:"actor_id"`
	Sequence      int    `json:"sequence"`
	Scene         string `json:"scene"`
	HandoverAt    time.Time `json:"handover_at"`
	NeedByAt      time.Time `json:"need_by_at"`
	ReturnAt      time.Time `json:"return_at"`
	ChangePoint   string `json:"change_point"`
	Force         bool   `json:"force"` // 明知有冲突仍强行排单（需二次确认）
}

// CreateAssignment 安排一次换装交接，并做三重校验：
//  1. 衣服必须可用（散场后未洗护 / 洗护维修中 / 少件破损均拦截）；
//  2. 换装耗时；3. 同件衣服转场+清洁间隔；4. 演员赶场。
func (s *Service) CreateAssignment(in *AssignmentInput) (*models.Assignment, []Conflict, error) {
	var c models.Costume
	if err := s.DB.First(&c, in.CostumeID).Error; err != nil {
		return nil, nil, ErrNotFound
	}
	var a models.Actor
	if err := s.DB.First(&a, in.ActorID).Error; err != nil {
		return nil, nil, ErrNotFound
	}
	var p models.Performance
	if err := s.DB.First(&p, in.PerformanceID).Error; err != nil {
		return nil, nil, ErrNotFound
	}

	var hard []Conflict
	// 状态门：未经洗护 / 洗护维修中 / 少件破损的衣服不能再次分配。
	switch c.Status {
	case models.CostumeAvailable:
	default:
		hard = append(hard, Conflict{Code: statusConflictCode(c.Status),
			Message: fmt.Sprintf("服装「%s %s」当前状态为 %s（位置：%s），不可再次分配；%s",
				c.Code, c.Name, statusText(c.Status), c.CurrentLocation, statusHint(c.Status))})
	}
	if in.NeedByAt.Before(in.HandoverAt) || in.ReturnAt.Before(in.HandoverAt) {
		hard = append(hard, Conflict{Code: ConflictChangeTime,
			Message: "交接/上场/交回时间顺序不合法"})
	}
	if len(hard) > 0 {
		return nil, nil, conflictErr(hard)
	}

	as := &models.Assignment{
		PerformanceID:  in.PerformanceID,
		CostumeID:      in.CostumeID,
		ActorID:        in.ActorID,
		PlannedActorID: in.ActorID,
		Sequence:       in.Sequence,
		Scene:          in.Scene,
		HandoverAt:     in.HandoverAt,
		NeedByAt:       in.NeedByAt,
		ReturnAt:       in.ReturnAt,
		ChangePoint:    in.ChangePoint,
		Status:         models.TaskPending,
	}
	if err := s.DB.Create(as).Error; err != nil {
		return nil, nil, err
	}

	items, err := loadAssignments(s.DB, in.PerformanceID)
	if err != nil {
		return nil, nil, err
	}
	cs := DetectConflicts(items)
	// 只拦截与新任务相关的冲突，避免历史已确认排单阻塞新建。
	related := relatedConflicts(cs, as.ID)
	if len(related) > 0 && !in.Force {
		s.DB.Delete(as)
		return nil, nil, conflictErr(related)
	}
	return as, cs, nil
}

func relatedConflicts(cs []Conflict, id uint) []Conflict {
	var out []Conflict
	for _, c := range cs {
		// 成对冲突（转场/清洁/赶场）同时关联前后两棒，新任务无论插在
		// 哪一棒都必须被识别并拦截。
		for _, rid := range c.RelatedIDs {
			if rid == id {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func statusConflictCode(status string) string {
	switch status {
	case models.CostumeReturned:
		return ConflictUnwashed
	case models.CostumeWashing, models.CostumeRepairing:
		return "care_in_progress"
	case models.CostumeLost, models.CostumeDamaged:
		return "incident_open"
	case models.CostumeRetired:
		return "retired"
	case models.CostumeInUse:
		return "in_use"
	default:
		return "not_available"
	}
}

func statusText(s string) string {
	switch s {
	case models.CostumeAvailable:
		return "可用"
	case models.CostumeInUse:
		return "穿着中"
	case models.CostumeReturned:
		return "已交回待洗护"
	case models.CostumeWashing:
		return "洗护中"
	case models.CostumeRepairing:
		return "维修中"
	case models.CostumeLost:
		return "少件"
	case models.CostumeDamaged:
		return "破损"
	case models.CostumeRetired:
		return "已报废"
	}
	return s
}

func statusHint(s string) string {
	switch s {
	case models.CostumeReturned:
		return "须完成洗护并恢复可用后才能排单"
	case models.CostumeWashing:
		return "洗护完成后自动恢复可用"
	case models.CostumeRepairing:
		return "维修完成后自动恢复可用"
	case models.CostumeLost:
		return "需先处理少件事件（找回并洗护）"
	case models.CostumeDamaged:
		return "需先处理破损事件（维修/洗护）"
	}
	return ""
}

// ----------------------------- 临时换角 -----------------------------

type RecastInput struct {
	ActorID uint   `json:"actor_id"`
	Reason  string `json:"reason"`
}

// Recast 临时换角。只允许调整尚未执行（pending）的任务；
// 已签收的记录永久关联实际签收演员，后端拒绝改动。
func (s *Service) Recast(assignmentID uint, in *RecastInput) (*models.Assignment, []Conflict, error) {
	var as models.Assignment
	if err := s.DB.First(&as, assignmentID).Error; err != nil {
		return nil, nil, ErrNotFound
	}
	var to models.Actor
	if err := s.DB.First(&to, in.ActorID).Error; err != nil {
		return nil, nil, ErrNotFound
	}
	if as.Status != models.TaskPending {
		return nil, nil, conflictErr([]Conflict{{
			Code:        ConflictSigned,
			AssignmentID: as.ID,
			Message: fmt.Sprintf("任务 #%d 已被 %s（状态：%s），签收记录仍关联原演员，不允许临时换角；仅可调整尚未执行的任务",
				as.ID, actorName(s.DB, as.ActorID), taskStatusText(as.Status)),
		}})
	}
	if as.ActorID == in.ActorID {
		return &as, nil, errors.New("新演员与原演员相同，无需换角")
	}

	from := as.ActorID
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		as.ActorID = in.ActorID
		if err := tx.Save(&as).Error; err != nil {
			return err
		}
		log := models.RecastLog{
			AssignmentID: as.ID, PerformanceID: as.PerformanceID,
			CostumeID: as.CostumeID, FromActorID: from, ToActorID: in.ActorID,
			Reason: in.Reason, ChangedAt: time.Now(),
		}
		return tx.Create(&log).Error
	})
	if err != nil {
		return nil, nil, err
	}
	items, err := loadAssignments(s.DB, as.PerformanceID)
	if err != nil {
		return nil, nil, err
	}
	cs := DetectConflicts(items)
	s.DB.Preload("Actor").Preload("Costume").First(&as, as.ID)
	return &as, cs, nil
}

func actorName(db *gorm.DB, id uint) string {
	var a models.Actor
	if err := db.First(&a, id).Error; err != nil {
		return "#" + itoa(id)
	}
	return a.Name
}

func taskStatusText(s string) string {
	switch s {
	case models.TaskPending:
		return "待执行"
	case models.TaskSigned:
		return "已签收"
	case models.TaskCollected:
		return "已交回"
	case models.TaskSkipped:
		return "已取消"
	}
	return s
}

// ----------------------------- 扫码（本地模拟） -----------------------------

type ScanInput struct {
	Code         string `json:"code"`
	AssignmentID uint   `json:"assignment_id"` // 可选：指定交接任务
	Action       string `json:"action"`        // sign=签收穿上 / collect=散场交回
	Operator     string `json:"operator"`
}

type ScanResult struct {
	OK         bool                 `json:"ok"`
	Action     string               `json:"action"`
	Costume    models.Costume       `json:"costume"`
	Assignment *models.Assignment   `json:"assignment,omitempty"`
	NextUser   *NextUser            `json:"next_user,omitempty"`
	Message    string               `json:"message"`
}

// NextUser 衣服的下一位使用人信息（场次页/扫码后展示）。
type NextUser struct {
	AssignmentID uint      `json:"assignment_id"`
	ActorID      uint      `json:"actor_id"`
	ActorName    string    `json:"actor_name"`
	Scene        string    `json:"scene"`
	HandoverAt   time.Time `json:"handover_at"`
	ChangePoint  string    `json:"change_point"`
}

// Scan 本地模拟扫码枪：扫到服装条码后执行签收或交回。
func (s *Service) Scan(in *ScanInput) (*ScanResult, error) {
	var c models.Costume
	if err := s.DB.Where("code = ?", in.Code).First(&c).Error; err != nil {
		return nil, fmt.Errorf("条码 %s 未登记", in.Code)
	}

	q := s.DB.Where("costume_id = ? AND status <> ?", c.ID, models.TaskSkipped).
		Preload("Actor").Preload("Costume")
	if in.AssignmentID != 0 {
		q = q.Where("id = ?", in.AssignmentID)
	}

	switch in.Action {
	case "sign":
		var as models.Assignment
		if err := q.Where("status = ?", models.TaskPending).
			Order("handover_at").First(&as).Error; err != nil {
			return nil, fmt.Errorf("服装 %s 没有可签收的待执行交接任务", c.Code)
		}
		if c.Status != models.CostumeAvailable {
			return nil, conflictErr([]Conflict{{Code: statusConflictCode(c.Status),
				Message: fmt.Sprintf("扫码拦截：服装「%s %s」%s，%s，不能签收上场",
					c.Code, c.Name, statusText(c.Status), c.CurrentLocation)}})
		}
		now := time.Now()
		actor := as.ActorID
		as.Status = models.TaskSigned
		as.SignedAt = &now
		as.SignedActorID = &actor
		as.ScanCode = c.Code
		if err := s.DB.Save(&as).Error; err != nil {
			return nil, err
		}
		c.Status = models.CostumeInUse
		c.CurrentLocation = as.Actor.Name + " 身上（" + as.ChangePoint + "）"
		if err := s.DB.Save(&c).Error; err != nil {
			return nil, err
		}
		res := &ScanResult{OK: true, Action: "sign", Costume: c, Assignment: &as,
			Message: fmt.Sprintf("已签收：%s 穿上「%s」", as.Actor.Name, c.Name)}
		res.NextUser = s.nextUser(c.ID, as.ID)
		return res, nil

	case "collect":
		var as models.Assignment
		if err := q.Where("status = ?", models.TaskSigned).
			Order("handover_at desc").First(&as).Error; err != nil {
			return nil, fmt.Errorf("服装 %s 没有已签收待交回的任务", c.Code)
		}
		now := time.Now()
		as.Status = models.TaskCollected
		as.CollectedAt = &now
		if err := s.DB.Save(&as).Error; err != nil {
			return nil, err
		}
		c.Status = models.CostumeReturned
		c.CurrentLocation = "散场待清点"
		if err := s.DB.Save(&c).Error; err != nil {
			return nil, err
		}
		return &ScanResult{OK: true, Action: "collect", Costume: c, Assignment: &as,
			NextUser: s.nextUser(c.ID, as.ID),
			Message:  fmt.Sprintf("已从 %s 处收回「%s」，待散场清点与洗护", as.Actor.Name, c.Name)}, nil

	default:
		// 只扫码不动作：返回衣服现状 + 下一位使用人，模拟扫码枪查询。
		var next models.Assignment
		hasNext := s.DB.Where("costume_id = ? AND status = ?", c.ID, models.TaskPending).
			Preload("Actor").Order("handover_at").First(&next).Error == nil
		res := &ScanResult{OK: true, Action: "lookup", Costume: c,
			Message: fmt.Sprintf("服装「%s %s」当前位置：%s，状态：%s", c.Code, c.Name, c.CurrentLocation, statusText(c.Status))}
		if hasNext {
			res.NextUser = &NextUser{AssignmentID: next.ID, ActorID: next.ActorID,
				ActorName: next.Actor.Name, Scene: next.Scene, HandoverAt: next.HandoverAt,
				ChangePoint: next.ChangePoint}
		}
		return res, nil
	}
}

// nextUser 返回某任务之后同一件衣服的下一位使用人。
func (s *Service) nextUser(costumeID, afterAssignmentID uint) *NextUser {
	var cur models.Assignment
	if err := s.DB.First(&cur, afterAssignmentID).Error; err != nil {
		return nil
	}
	var next models.Assignment
	if err := s.DB.Where("costume_id = ? AND status = ? AND handover_at > ?",
		costumeID, models.TaskPending, cur.HandoverAt).
		Preload("Actor").Order("handover_at").First(&next).Error; err != nil {
		return nil
	}
	return &NextUser{AssignmentID: next.ID, ActorID: next.ActorID,
		ActorName: next.Actor.Name, Scene: next.Scene, HandoverAt: next.HandoverAt,
		ChangePoint: next.ChangePoint}
}

// ----------------------------- 洗护 / 维修 -----------------------------

type StartCareInput struct {
	Type     string `json:"type"` // wash / repair
	Reason   string `json:"reason"`
	Operator string `json:"operator"`
}

// StartCare 开始洗护或维修。有未关闭少件/破损事件的衣服不能直接送去洗护。
func (s *Service) StartCare(costumeID uint, in *StartCareInput) (*models.CareRecord, error) {
	var c models.Costume
	if err := s.DB.First(&c, costumeID).Error; err != nil {
		return nil, ErrNotFound
	}
	if in.Type != "wash" && in.Type != "repair" {
		return nil, errors.New("type 必须是 wash 或 repair")
	}
	if open := s.openIncidentCount(c.ID); open > 0 {
		return nil, fmt.Errorf("服装 %s 有 %d 个未处理的少件/破损事件，须先处理事件", c.Code, open)
	}
	if c.Status == models.CostumeWashing || c.Status == models.CostumeRepairing {
		return nil, errors.New("该服装已有进行中的洗护/维修单")
	}
	rec := models.CareRecord{CostumeID: c.ID, Type: in.Type, Status: "ongoing",
		Reason: in.Reason, Operator: in.Operator, StartedAt: time.Now()}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&rec).Error; err != nil {
			return err
		}
		c.Status = map[string]string{"wash": models.CostumeWashing, "repair": models.CostumeRepairing}[in.Type]
		c.CurrentLocation = map[string]string{"wash": "洗衣房", "repair": "维修间"}[in.Type]
		return tx.Save(&c).Error
	})
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// CompleteCare 完成洗护/维修 —— 衣服恢复可用的唯一入口。
func (s *Service) CompleteCare(careID uint, operator string) (*models.Costume, error) {
	var rec models.CareRecord
	if err := s.DB.First(&rec, careID).Error; err != nil {
		return nil, ErrNotFound
	}
	if rec.Status == "done" {
		return nil, errors.New("该洗护/维修单已完成")
	}
	if open := s.openIncidentCount(rec.CostumeID); open > 0 {
		return nil, fmt.Errorf("仍有 %d 个少件/破损事件未关闭，不能恢复可用", open)
	}
	var c models.Costume
	if err := s.DB.First(&c, rec.CostumeID).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		rec.Status = "done"
		rec.CompletedAt = &now
		if err := tx.Save(&rec).Error; err != nil {
			return err
		}
		c.Status = models.CostumeAvailable
		c.CurrentLocation = "主换装间"
		if rec.Type == "wash" {
			c.LastCleanedAt = &now
		}
		return tx.Save(&c).Error
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) ListCare(costumeID uint) ([]models.CareRecord, error) {
	var out []models.CareRecord
	err := s.DB.Where("costume_id = ?", costumeID).Order("start_at desc").Find(&out).Error
	return out, err
}

// ----------------------------- 散场清点 / 少件破损 -----------------------------

type CheckItem struct {
	CostumeID uint   `json:"costume_id"`
	Found     bool   `json:"found"`
	Condition string `json:"condition"` // ok / damaged
	Note      string `json:"note"`
}

type CheckResult struct {
	Check    models.InventoryCheck `json:"check"`
	Incident *models.Incident      `json:"incident,omitempty"`
	Blocked  string                `json:"blocked,omitempty"`
}

// CheckOne 散场逐件清点。少件/破损必须开事件处理单，
// 系统不提供“调整库存数量”来抵消问题。
func (s *Service) CheckOne(perfID uint, in *CheckItem) (*CheckResult, error) {
	var c models.Costume
	if err := s.DB.First(&c, in.CostumeID).Error; err != nil {
		return nil, ErrNotFound
	}
	// 一件衣服同场只清点一次，杜绝重复清点把问题“点没了”。
	var dup int64
	s.DB.Model(&models.InventoryCheck{}).
		Where("performance_id = ? AND costume_id = ?", perfID, c.ID).Count(&dup)
	if dup > 0 {
		return nil, fmt.Errorf("服装 %s 本场已清点过；如有异议请在事件单中跟进", c.Code)
	}

	check := models.InventoryCheck{PerformanceID: perfID, CostumeID: c.ID,
		Found: in.Found, Condition: in.Condition, CheckedAt: time.Now(), Note: in.Note}

	res := &CheckResult{}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if !in.Found {
			inc := models.Incident{PerformanceID: &perfID, CostumeID: c.ID,
				Type: models.IncidentLost, Status: models.IncidentOpen,
				Description: nv(in.Note, "散场清点未见实物"),
				ReportedAt: time.Now()}
			if err := tx.Create(&inc).Error; err != nil {
				return err
			}
			check.Found = false
			check.IncidentID = &inc.ID
			c.Status = models.CostumeLost
			c.CurrentLocation = "去向不明"
			res.Incident = &inc
		} else if in.Condition == "damaged" {
			inc := models.Incident{PerformanceID: &perfID, CostumeID: c.ID,
				Type: models.IncidentDamaged, Status: models.IncidentOpen,
				Description: nv(in.Note, "散场清点发现破损"),
				ReportedAt: time.Now()}
			if err := tx.Create(&inc).Error; err != nil {
				return err
			}
			check.Found = true
			check.Condition = "damaged"
			check.IncidentID = &inc.ID
			c.Status = models.CostumeDamaged
			c.CurrentLocation = "维修间待检"
			res.Incident = &inc
		} else {
			check.Found = true
			check.Condition = "ok"
			// 完好交回也必须洗护后才能再用。
			c.Status = models.CostumeReturned
			c.CurrentLocation = "洗衣房待洗篮"
		}
		if err := tx.Create(&check).Error; err != nil {
			return err
		}
		return tx.Save(&c).Error
	})
	if err != nil {
		return nil, err
	}
	res.Check = check
	return res, nil
}

func nv(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (s *Service) openIncidentCount(costumeID uint) int64 {
	var n int64
	s.DB.Model(&models.Incident{}).
		Where("costume_id = ? AND status = ?", costumeID, models.IncidentOpen).Count(&n)
	return n
}

func (s *Service) ListIncidents(status string) ([]models.Incident, error) {
	var out []models.Incident
	q := s.DB.Preload("Costume").Order("reported_at desc")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Find(&out).Error
	return out, err
}

type ResolveIncidentInput struct {
	Resolution string `json:"resolution"` // found / repaired / written_off
	Note       string `json:"note"`
	Handler    string `json:"handler"`
}

// ResolveIncident 处理少件/破损事件。处理结果不会直接把衣服变回可用：
// 找回→待洗护；修复→须走维修单完成；报废→retired。
// “补一个库存数量”在系统中不存在。
func (s *Service) ResolveIncident(id uint, in *ResolveIncidentInput) (*models.Incident, *models.Costume, error) {
	var inc models.Incident
	if err := s.DB.First(&inc, id).Error; err != nil {
		return nil, nil, ErrNotFound
	}
	if inc.Status != models.IncidentOpen {
		return nil, nil, errors.New("事件已处理")
	}
	var c models.Costume
	if err := s.DB.First(&c, inc.CostumeID).Error; err != nil {
		return nil, nil, err
	}
	now := time.Now()
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		inc.ResolvedAt = &now
		inc.Handler = in.Handler
		inc.Resolution = in.Resolution
		inc.Status = models.IncidentResolved
		note := in.Note
		switch in.Resolution {
		case "found":
			inc.Status = models.IncidentResolved
			c.Status = models.CostumeReturned
			c.CurrentLocation = "洗衣房待洗篮"
			note = "已找回，仍须洗护后才能再用；" + note
		case "repaired":
			// 开维修单并立即完成，服装才恢复可用（“维修完成后恢复可用”）。
			rec := models.CareRecord{CostumeID: c.ID, Type: "repair", Status: "done",
				Reason: "事件 #" + itoa(inc.ID) + " 破损维修", Operator: in.Handler,
				StartedAt: now, CompletedAt: &now}
			if err := tx.Create(&rec).Error; err != nil {
				return err
			}
			c.Status = models.CostumeAvailable
			c.CurrentLocation = "主换装间"
			note = "已维修并检验合格，恢复可用；" + note
		case "written_off":
			inc.Status = models.IncidentWritten
			c.Status = models.CostumeRetired
			c.CurrentLocation = "报废品库"
			note = "报废处理；" + note
		default:
			return errors.New("resolution 必须是 found / repaired / written_off")
		}
		if note != "" {
			inc.Description = inc.Description + "｜处理：" + note
		}
		if err := tx.Save(&inc).Error; err != nil {
			return err
		}
		return tx.Save(&c).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &inc, &c, nil
}

func (s *Service) ListChecks(perfID uint) ([]models.InventoryCheck, error) {
	var out []models.InventoryCheck
	err := s.DB.Where("performance_id = ?", perfID).
		Preload("Costume").Order("checked_at").Find(&out).Error
	return out, err
}

// ----------------------------- 场次总览 -----------------------------

// CostumeRoute 一件衣服在场次中的路线视图。
type CostumeRoute struct {
	Costume   models.Costume       `json:"costume"`
	NextUser  *NextUser            `json:"next_user"`
	Legs      []models.Assignment  `json:"legs"`
	Conflict  []Conflict           `json:"conflicts"`
}

type PerformanceDetail struct {
	Performance models.Performance `json:"performance"`
	Assignments []models.Assignment `json:"assignments"`
	Costumes    []CostumeRoute     `json:"costumes"`
	Conflicts   []Conflict         `json:"conflicts"`
	Checks      []models.InventoryCheck `json:"checks"`
	Summary     map[string]int     `json:"summary"`
}

// GetPerformance 场次页面：服装当前位置、下一位使用人、全部时间冲突。
func (s *Service) GetPerformance(id uint) (*PerformanceDetail, error) {
	var p models.Performance
	if err := s.DB.First(&p, id).Error; err != nil {
		return nil, ErrNotFound
	}
	var as []models.Assignment
	if err := s.DB.Where("performance_id = ?", id).
		Preload("Costume").Preload("Actor").Order("sequence, handover_at").Find(&as).Error; err != nil {
		return nil, err
	}
	active := make([]loaded, 0, len(as))
	for _, a := range as {
		if a.Status != models.TaskSkipped {
			active = append(active, loaded{A: a, C: a.Costume})
		}
	}
	cs := DetectConflicts(active)

	// 按服装聚合路线。
	routes := map[uint]*CostumeRoute{}
	for _, it := range active {
		r, ok := routes[it.A.CostumeID]
		if !ok {
			// 用服装表的实时状态（位置/洗护状态），而非任务快照。
			var live models.Costume
			s.DB.First(&live, it.A.CostumeID)
			r = &CostumeRoute{Costume: live}
			routes[it.A.CostumeID] = r
		}
		r.Legs = append(r.Legs, it.A)
	}
	for _, it := range active {
		r := routes[it.A.CostumeID]
		for _, c := range cs {
			if c.AssignmentID == it.A.ID {
				r.Conflict = append(r.Conflict, c)
			}
		}
	}
	out := make([]CostumeRoute, 0, len(routes))
	for _, r := range routes {
		sort.Slice(r.Legs, func(i, j int) bool { return r.Legs[i].Sequence < r.Legs[j].Sequence })
		// 下一位使用人 = 尚未签收的最早一棒。
		for i := range r.Legs {
			if r.Legs[i].Status == models.TaskPending {
				leg := r.Legs[i]
				r.NextUser = &NextUser{AssignmentID: leg.ID, ActorID: leg.ActorID,
					ActorName: leg.Actor.Name, Scene: leg.Scene, HandoverAt: leg.HandoverAt,
					ChangePoint: leg.ChangePoint}
				break
			}
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Costume.Code < out[j].Costume.Code })

	checks, _ := s.ListChecks(id)

	sum := map[string]int{
		"assignments": len(active),
		"pending":     0, "signed": 0, "collected": 0,
		"conflicts": len(cs),
	}
	for _, a := range as {
		sum[a.Status]++
	}
	return &PerformanceDetail{Performance: p, Assignments: as, Costumes: out,
		Conflicts: cs, Checks: checks, Summary: sum}, nil
}
