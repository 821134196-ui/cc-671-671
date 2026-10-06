package service

import (
	"time"

	"theater-costumes/internal/models"
)

type TaskView struct {
	models.Task
	CostumeCode    string `json:"costumeCode"`
	CostumeName    string `json:"costumeName"`
	RoleName       string `json:"roleName"`
	ActorName      string `json:"actorName"`
	OriginalActor  string `json:"originalActor"`
	SignedActor    string `json:"signedActor"`
	SceneName      string `json:"sceneName"`
	CostumeStatus  string `json:"costumeStatus"`
	Conflicts      []Conflict `json:"conflicts"`
}

type CostumeView struct {
	models.Costume
	HolderName      string     `json:"holderName"`
	NextActor       string     `json:"nextActor"`
	NextRole        string     `json:"nextRole"`
	NextOnAt        *time.Time `json:"nextOnAt"`
	NextChangePoint string     `json:"nextChangePoint"`
	OpenCare        string     `json:"openCare"`
	OpenIssue       string     `json:"openIssue"`
}

type Board struct {
	Show      models.Show  `json:"show"`
	Roles     []models.Role   `json:"roles"`
	Scenes    []models.Scene  `json:"scenes"`
	Tasks     []TaskView   `json:"tasks"`
	Costumes  []CostumeView `json:"costumes"`
	Conflicts []Conflict   `json:"conflicts"`
	Now       time.Time    `json:"now"`
}

func (s *Service) GetBoard(showID uint) (*Board, error) {
	var show models.Show
	if err := s.DB.First(&show, showID).Error; err != nil {
		return nil, errNoShow
	}
	var roles []models.Role
	s.DB.Where("show_id = ?", showID).Find(&roles)
	var scenes []models.Scene
	s.DB.Where("show_id = ?", showID).Order("seq asc").Find(&scenes)
	var tasks []models.Task
	s.DB.Where("show_id = ? AND status <> ?", showID, models.TaskCancelled).
		Order("on_at asc, seq asc").Find(&tasks)
	var costumes []models.Costume
	s.DB.Order("code asc").Find(&costumes)

	actors := map[uint]string{}
	var allActors []models.Actor
	s.DB.Find(&allActors)
	for _, a := range allActors {
		actors[a.ID] = a.Name
	}
	roleNames := map[uint]string{}
	for _, r := range roles {
		roleNames[r.ID] = r.Name
	}
	sceneNames := map[uint]string{}
	for _, sc := range scenes {
		sceneNames[sc.ID] = sc.Name
	}
	costumeMap := map[uint]models.Costume{}
	for _, c := range costumes {
		costumeMap[c.ID] = c
	}

	now := time.Now()
	taskViews := make([]TaskView, 0, len(tasks))
	allConflicts := []Conflict{}
	for _, t := range tasks {
		cs, _ := EvaluateTask(s.DB, show, t)
		// 已签收/交回的任务：路线已执行，不再报告路线冲突，只展示保存时记录的冲突
		if t.Status != models.TaskPending {
			cs = nil
		}
		allConflicts = append(allConflicts, cs...)
		c := costumeMap[t.CostumeID]
		var signedName string
		if t.SignedByActorID != nil {
			signedName = actors[*t.SignedByActorID]
		}
		taskViews = append(taskViews, TaskView{
			Task:           t,
			CostumeCode:    c.Code,
			CostumeName:    c.Name,
			RoleName:       roleNames[t.RoleID],
			ActorName:      actors[t.ActorID],
			OriginalActor:  actors[t.OriginalActorID],
			SignedActor:    signedName,
			SceneName:      sceneNames[t.SceneID],
			CostumeStatus:  c.Status,
			Conflicts:      cs,
		})
	}

	costumeViews := make([]CostumeView, 0, len(costumes))
	for _, c := range costumes {
		v := CostumeView{Costume: c}
		if c.HolderID != nil {
			v.HolderName = actors[*c.HolderID]
		}
		// 下一位使用人：该戏服最早的待签收任务（无论是否已过计划时刻）
		for i := range tasks {
			t := tasks[i]
			if t.CostumeID != c.ID || t.Status != models.TaskPending {
				continue
			}
			if v.NextOnAt == nil || t.OnAt.Before(*v.NextOnAt) {
				on := t.OnAt
				v.NextOnAt = &on
				v.NextActor = actors[t.ActorID]
				v.NextRole = roleNames[t.RoleID]
				v.NextChangePoint = t.ChangePoint
			}
		}
		var care models.CareRecord
		if err := s.DB.Where("costume_id = ? AND status = ?", c.ID, models.CareOpen).Order("id desc").First(&care).Error; err == nil {
			if care.Type == models.CareLaundry {
				v.OpenCare = "洗护中"
			} else {
				v.OpenCare = "维修中"
			}
		}
		var issue models.Issue
		if err := s.DB.Where("costume_id = ? AND status = ?", c.ID, models.IssueOpen).Order("id desc").First(&issue).Error; err == nil {
			if issue.Type == models.IssueMissing {
				v.OpenIssue = "少件待处理"
			} else {
				v.OpenIssue = "破损待处理"
			}
		}
		costumeViews = append(costumeViews, v)
	}

	return &Board{
		Show: show, Roles: roles, Scenes: scenes, Tasks: taskViews,
		Costumes: costumeViews, Conflicts: dedupeConflicts(allConflicts), Now: now,
	}, nil
}

func dedupeConflicts(cs []Conflict) []Conflict {
	seen := map[string]bool{}
	out := []Conflict{}
	for _, c := range cs {
		key := c.Type + "|" + c.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

var errNoShow = newErr("演出不存在")

type bizErr struct{ msg string }

func (e *bizErr) Error() string { return e.msg }
func newErr(msg string) error   { return &bizErr{msg: msg} }
