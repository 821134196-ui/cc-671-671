package service

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"theater-costume/internal/models"

	"gorm.io/gorm"
)

// 转场缓冲：衣服从演员交回点送到下一个换装点所需的最少时间（秒）。
const TransferSeconds = 120

// 冲突类型代码。
const (
	ConflictChangeTime  = "change_time"  // 单个任务：签收后到上场来不及换好
	ConflictTransfer    = "transfer"     // 同一件衣服转场时间不足，不能及时到位
	ConflictClean       = "clean"        // 不同演员共用，清洁间隔不足
	ConflictActorOverlap = "actor_overlap" // 同一演员两处上场时间重叠
	ConflictActorRush   = "actor_rush"   // 同一演员连续上场来不及换装
	ConflictUnwashed    = "unwashed"     // 衣服散场后未洗护即被再次分配
	ConflictSigned      = "signed_locked" // 已签收任务被尝试换角
)

// Conflict 描述一条换装路线上的时间/状态冲突。
type Conflict struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	AssignmentID uint  `json:"assignment_id,omitempty"`
	// RelatedIDs 涉及的全部交接任务（成对冲突包含前后两棒），
	// 供建单时判断冲突是否由新任务引入、前端在各棒上同时高亮。
	RelatedIDs []uint `json:"related_ids,omitempty"`
}

func newConflict(code, msg string, ids ...uint) Conflict {
	var primary uint
	if len(ids) > 0 {
		primary = ids[0]
	}
	return Conflict{Code: code, Message: msg, AssignmentID: primary, RelatedIDs: ids}
}

// ConflictError 建单/换角触发硬拦截时返回（API 层映射为 400）。
type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	if len(e.Conflicts) == 0 {
		return "存在冲突"
	}
	return e.Conflicts[0].Message
}

func conflictErr(cs []Conflict) error {
	if len(cs) == 0 {
		return nil
	}
	return &ConflictError{Conflicts: cs}
}

// IsConflict 供 API 层判断错误类型。
func IsConflict(err error) (*ConflictError, bool) {
	var ce *ConflictError
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}

type loaded struct {
	A models.Assignment
	C models.Costume
}

// loadAssignments 载入某场全部交接任务及服装、演员信息。
func loadAssignments(db *gorm.DB, perfID uint) ([]loaded, error) {
	var as []models.Assignment
	if err := db.Where("performance_id = ?", perfID).
		Preload("Costume").Preload("Actor").
		Find(&as).Error; err != nil {
		return nil, err
	}
	out := make([]loaded, 0, len(as))
	for _, a := range as {
		if a.Status == models.TaskSkipped {
			continue
		}
		out = append(out, loaded{A: a, C: a.Costume})
	}
	return out, nil
}

// DetectConflicts 对一场演出的完整换装路线做静态检测，
// 既用于建单时硬拦截，也用于场次页面展示。
func DetectConflicts(items []loaded) []Conflict {
	var cs []Conflict

	// 0) 待执行任务对应的实物服装当前必须可到位：
	//    未洗护 / 洗护维修中 / 少件破损都属于阻断性问题。
	for _, it := range items {
		if it.A.Status != models.TaskPending {
			continue
		}
		switch it.C.Status {
		case models.CostumeAvailable, models.CostumeInUse:
			// available：正常；in_use：正在本场前一棒穿着，属于路线内流转。
		case models.CostumeReturned:
			cs = append(cs, newConflict(ConflictUnwashed,
				"服装「"+it.C.Code+" "+it.C.Name+"」散场交回后尚未洗护"+
					"（位置："+it.C.CurrentLocation+"），不能再次分配给 "+it.A.Actor.Name,
				it.A.ID))
		case models.CostumeWashing, models.CostumeRepairing:
			cs = append(cs, newConflict("care_in_progress",
				"服装「"+it.C.Code+" "+it.C.Name+"」正在"+
					map[string]string{models.CostumeWashing: "洗护", models.CostumeRepairing: "维修"}[it.C.Status]+
					"（位置："+it.C.CurrentLocation+"），无法按时到位",
				it.A.ID))
		case models.CostumeLost, models.CostumeDamaged:
			cs = append(cs, newConflict("incident_open",
				"服装「"+it.C.Code+" "+it.C.Name+"」存在未处理的"+
					map[string]string{models.CostumeLost: "少件", models.CostumeDamaged: "破损"}[it.C.Status]+
					"事件，不能上场",
				it.A.ID))
		case models.CostumeRetired:
			cs = append(cs, newConflict("retired",
				"服装「"+it.C.Code+" "+it.C.Name+"」已报废", it.A.ID))
		}
	}

	// 1) 单任务换装耗时：签收时间 + 预计换装耗时必须早于上场 deadline。
	for _, it := range items {
		need := it.A.NeedByAt.Sub(it.A.HandoverAt)
		if need < 0 {
			cs = append(cs, newConflict(ConflictChangeTime,
				"任务 #"+itoa(it.A.ID)+"：上场时间早于交接时间", it.A.ID))
		} else if int(need.Seconds()) < it.C.ChangeSeconds {
			cs = append(cs, newConflict(ConflictChangeTime,
				"任务 #"+itoa(it.A.ID)+"：签收后仅剩 "+
					formatDur(need)+"，服装「"+it.C.Name+"」需要 "+
					formatDur(time.Duration(it.C.ChangeSeconds)*time.Second)+"，来不及换装", it.A.ID))
		}
	}

	// 2) 同一件实物衣服的交接链：按交接时间排序，检查转场与清洁间隔。
	byCostume := map[uint][]loaded{}
	for _, it := range items {
		byCostume[it.A.CostumeID] = append(byCostume[it.A.CostumeID], it)
	}
	for _, list := range byCostume {
		sort.Slice(list, func(i, j int) bool { return list[i].A.HandoverAt.Before(list[j].A.HandoverAt) })
		for i := 1; i < len(list); i++ {
			prev, next := list[i-1], list[i]
			gap := next.A.HandoverAt.Sub(prev.A.ReturnAt)
			if gap < TransferSeconds*time.Second {
				cs = append(cs, newConflict(ConflictTransfer,
					"服装「"+next.C.Code+" "+next.C.Name+"」"+
						"前一位 "+prev.A.Actor.Name+" 于 "+ft(prev.A.ReturnAt)+" 才交回，"+
						"下一位 "+next.A.Actor.Name+" "+ft(next.A.HandoverAt)+" 就要用，"+
						"转场仅 "+formatDur(gap)+"（至少 "+formatDur(TransferSeconds*time.Second)+"），衣服不能及时到位",
					next.A.ID, prev.A.ID))
				continue
			}
			// 同一件衣服给不同角色/演员共用时，检查清洁间隔。
			if prev.A.ActorID != next.A.ActorID &&
				int(gap.Seconds()) < next.C.CleanSeconds {
				cs = append(cs, newConflict(ConflictClean,
					"服装「"+next.C.Code+" "+next.C.Name+"」由 "+
						prev.A.Actor.Name+" 转给 "+next.A.Actor.Name+"，间隔 "+
						formatDur(gap)+" 短于清洁要求 "+
						formatDur(time.Duration(next.C.CleanSeconds)*time.Second),
					next.A.ID, prev.A.ID))
			}
		}
	}

	// 3) 同一演员的上场链：时间不能重叠，且两次上场间要够换下一套。
	byActor := map[uint][]loaded{}
	for _, it := range items {
		byActor[it.A.ActorID] = append(byActor[it.A.ActorID], it)
	}
	for _, list := range byActor {
		sort.Slice(list, func(i, j int) bool { return list[i].A.HandoverAt.Before(list[j].A.HandoverAt) })
		for i := 1; i < len(list); i++ {
			prev, next := list[i-1], list[i]
			if next.A.NeedByAt.Before(prev.A.ReturnAt) {
				cs = append(cs, newConflict(ConflictActorOverlap,
					"演员 "+next.A.Actor.Name+" 上下场时间重叠：前一套 "+
						ft(prev.A.ReturnAt)+" 才下场，下一场 "+ft(next.A.NeedByAt)+" 必须上场",
					next.A.ID, prev.A.ID))
				continue
			}
			gap := next.A.NeedByAt.Sub(prev.A.ReturnAt)
			if int(gap.Seconds()) < next.C.ChangeSeconds {
				cs = append(cs, newConflict(ConflictActorRush,
					"演员 "+next.A.Actor.Name+" 连续上场仅间隔 "+formatDur(gap)+
						"，换上「"+next.C.Name+"」需要 "+
						formatDur(time.Duration(next.C.ChangeSeconds)*time.Second)+"，来不及换装",
					next.A.ID, prev.A.ID))
			}
		}
	}
	return cs
}

func ft(t time.Time) string { return t.Format("01-02 15:04") }

func itoa(u uint) string {
	return strconv.FormatUint(uint64(u), 10)
}

func formatDur(d time.Duration) string {
	if d < 0 {
		return "-" + formatDur(-d)
	}
	s := int(d.Seconds())
	if s < 60 {
		return strconv.Itoa(s) + " 秒"
	}
	weeks := [...]struct {
		unit string
		secs int
	}{
		{"小时", 3600}, {"分钟", 60},
	}
	var parts []string
	rem := s
	for _, w := range weeks {
		if v := rem / w.secs; v > 0 {
			parts = append(parts, strconv.Itoa(v)+w.unit)
			rem -= v * w.secs
		}
	}
	if rem > 0 {
		parts = append(parts, strconv.Itoa(rem)+"秒")
	}
	return strings.Join(parts, "")
}
