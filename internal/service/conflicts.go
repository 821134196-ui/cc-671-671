package service

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"theater-costumes/internal/models"
)

// 冲突类型
const (
	ConflictActorOverlap      = "actor_overlap"      // 演员同时段两处上场
	ConflictActorChangeTight  = "actor_change_tight" // 演员来不及换装
	ConflictCostumeOverlap    = "costume_overlap"    // 同一件戏服同时段分给两人
	ConflictCostumeTurnover   = "costume_turnover"   // 转场/清洁间隔不足，衣服不能及时到位
	ConflictCostumeBlocked    = "costume_blocked"    // 洗护/维修/少件/破损未恢复
)

type Conflict struct {
	Type      string `json:"type"`
	Hard      bool   `json:"hard"` // hard=true 不允许强制保存
	Message   string `json:"message"`
	TaskID    *uint  `json:"taskId,omitempty"`
	CostumeID uint   `json:"costumeId,omitempty"`
}

// 评估单个任务与同场次其它任务之间的路线冲突
func EvaluateTask(db *gorm.DB, show models.Show, task models.Task) ([]Conflict, error) {
	var costume models.Costume
	if err := db.First(&costume, task.CostumeID).Error; err != nil {
		return nil, err
	}
	var others []models.Task
	if err := db.Where("show_id = ? AND status <> ? AND id <> ?",
		show.ID, models.TaskCancelled, task.ID).Find(&others).Error; err != nil {
		return nil, err
	}
	var allCostumes []models.Costume
	if err := db.Find(&allCostumes).Error; err != nil {
		return nil, err
	}
	changeSecs := map[uint]int64{}
	for _, c := range allCostumes {
		changeSecs[c.ID] = c.ChangeSeconds
	}
	var out []Conflict

	// 1) 戏服当前状态门禁：洗护/维修未完成、少件/破损未处理，一律不能再分配
	var openCare int64
	db.Model(&models.CareRecord{}).Where("costume_id = ? AND status = ?", costume.ID, models.CareOpen).Count(&openCare)
	var openIssue int64
	db.Model(&models.Issue{}).Where("costume_id = ? AND status = ?", costume.ID, models.IssueOpen).Count(&openIssue)
	if openCare > 0 {
		out = append(out, Conflict{
			Type:      ConflictCostumeBlocked,
			Hard:      true,
			CostumeID: costume.ID,
			Message: fmt.Sprintf("戏服 %s（%s）存在未完成的洗护/维修记录，完成前不能再次分配",
				costume.Code, costume.Name),
		})
	}
	if openIssue > 0 {
		out = append(out, Conflict{
			Type:      ConflictCostumeBlocked,
			Hard:      true,
			CostumeID: costume.ID,
			Message: fmt.Sprintf("戏服 %s（%s）有未处理的少件/破损问题单，处理完成前不能再次分配（不能靠补数量消除）",
				costume.Code, costume.Name),
		})
	}
	if costume.Status != models.CostumeAvailable && costume.Status != models.CostumeInUse && openCare == 0 && openIssue == 0 {
		out = append(out, Conflict{
			Type:      ConflictCostumeBlocked,
			Hard:      true,
			CostumeID: costume.ID,
			Message: fmt.Sprintf("戏服 %s（%s）当前状态为「%s」，恢复可用前不能分配",
				costume.Code, costume.Name, statusText(costume.Status)),
		})
	}

	changeStart := task.OnAt.Add(-time.Duration(costume.ChangeSeconds) * time.Second)

	for _, o := range others {
		sameCostume := o.CostumeID == task.CostumeID
		sameActor := o.ActorID == task.ActorID

		// 2) 时间区间重叠 [OnAt, OffAt)
		overlap := task.OnAt.Before(o.OffAt) && o.OnAt.Before(task.OffAt)
		if overlap {
			if sameActor && !sameCostume {
				out = append(out, Conflict{
					Type: ConflictActorOverlap, Hard: true, TaskID: &o.ID, CostumeID: costume.ID,
					Message: fmt.Sprintf("演员在 %s–%s 已有上场任务（戏服#%d），无法同时穿戴 %s",
						o.OnAt.Format("15:04"), o.OffAt.Format("15:04"), o.CostumeID, costume.Code),
				})
			}
			if sameCostume && !sameActor {
				out = append(out, Conflict{
					Type: ConflictCostumeOverlap, Hard: true, TaskID: &o.ID, CostumeID: costume.ID,
					Message: fmt.Sprintf("戏服 %s 在 %s–%s 已分配给另一位演员，时间重叠",
						costume.Code, o.OnAt.Format("15:04"), o.OffAt.Format("15:04")),
				})
			}
		}

		// 3) 演员换装路线：紧前任务下场 -> 本任务上场
		if sameActor && !o.OffAt.After(task.OnAt) {
			need := changeSecs[task.CostumeID]
			note := ""
			if o.ChangePoint != task.ChangePoint {
				need += show.TransferSeconds
				note = fmt.Sprintf("（含换装点转场 %d 分钟）", show.TransferSeconds/60)
			}
			readyAt := o.OffAt.Add(time.Duration(need) * time.Second)
			if readyAt.After(task.OnAt) {
				short := readyAt.Sub(task.OnAt).Round(time.Second)
				out = append(out, Conflict{
					Type: ConflictActorChangeTight, TaskID: &o.ID, CostumeID: costume.ID,
					Message: fmt.Sprintf("演员来不及换装：%s 下场后换装需 %d 分钟%s，最晚 %s 须就绪，但 %s 即上场，缺口 %s",
						o.ChangePoint, need/60, note,
						o.OffAt.Format("15:04"), task.OnAt.Format("15:04"), short),
				})
			}
		}
		// 演员换装路线：本任务 -> 紧后任务
		if sameActor && !task.OffAt.After(o.OnAt) {
			need := changeSecs[o.CostumeID]
			if o.ChangePoint != task.ChangePoint {
				need += show.TransferSeconds
			}
			readyAt := task.OffAt.Add(time.Duration(need) * time.Second)
			if readyAt.After(o.OnAt) {
				short := readyAt.Sub(o.OnAt).Round(time.Second)
				out = append(out, Conflict{
					Type: ConflictActorChangeTight, TaskID: &o.ID, CostumeID: costume.ID,
					Message: fmt.Sprintf("演员来不及换装：%s 下场后赶 %s 的上场，换装+转场需 %d 分钟，缺口 %s",
						task.ChangePoint, o.ChangePoint, need/60, short),
				})
			}
		}

		// 4) 戏服周转：紧前使用 -> 本任务换装开始（须拿到衣服）
		if sameCostume && !o.OffAt.After(changeStart) {
			need := int64(0)
			parts := ""
			if o.ChangePoint != task.ChangePoint {
				need += show.TransferSeconds
				parts = fmt.Sprintf("转场%d分钟", show.TransferSeconds/60)
			}
			if o.RoleID != task.RoleID {
				need += show.CleaningSeconds
				if parts != "" {
					parts += "、"
				}
				parts += fmt.Sprintf("清洁间隔%d分钟", show.CleaningSeconds/60)
			}
			readyAt := o.OffAt.Add(time.Duration(need) * time.Second)
			if readyAt.After(changeStart) {
				short := readyAt.Sub(changeStart).Round(time.Second)
				out = append(out, Conflict{
					Type: ConflictCostumeTurnover, TaskID: &o.ID, CostumeID: costume.ID,
					Message: fmt.Sprintf("戏服 %s 不能及时到位：上一处 %s 下场后需%s，最晚 %s 应到本换装点，缺口 %s",
						costume.Code, o.ChangePoint, parts, changeStart.Format("15:04"), short),
				})
			}
		}
		// 戏服周转：本任务 -> 紧后使用
		if sameCostume {
			nextStart := o.OnAt.Add(-time.Duration(changeSecs[o.CostumeID]) * time.Second)
			if !task.OffAt.After(nextStart) {
				need := int64(0)
				if o.ChangePoint != task.ChangePoint {
					need += show.TransferSeconds
				}
				if o.RoleID != task.RoleID {
					need += show.CleaningSeconds
				}
				readyAt := task.OffAt.Add(time.Duration(need) * time.Second)
				if readyAt.After(nextStart) {
					short := readyAt.Sub(nextStart).Round(time.Second)
					out = append(out, Conflict{
						Type: ConflictCostumeTurnover, TaskID: &o.ID, CostumeID: costume.ID,
						Message: fmt.Sprintf("戏服 %s 周转不及：交回后赶 %s 的换装需 %d 分钟，缺口 %s",
							costume.Code, o.ChangePoint, need/60, short),
					})
				}
			}
		}
	}
	return out, nil
}

func statusText(s string) string {
	switch s {
	case models.CostumeAvailable:
		return "可用"
	case models.CostumeInUse:
		return "使用中"
	case models.CostumeLaundering:
		return "洗护中"
	case models.CostumeRepairing:
		return "维修中"
	case models.CostumeMissing:
		return "少件"
	case models.CostumeDamaged:
		return "破损"
	}
	return s
}

func HasHard(cs []Conflict) bool {
	for _, c := range cs {
		if c.Hard {
			return true
		}
	}
	return false
}
