package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"theater-costumes/internal/models"
	"theater-costumes/internal/service"
)

type Handler struct{ Svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{Svc: svc} }

func (h *Handler) Register(r *gin.Engine) {
	api := r.Group("/api")
	{
		api.POST("/seed", h.Seed)

		api.GET("/shows", h.ListShows)
		api.POST("/shows", h.CreateShow)
		api.GET("/shows/:id/board", h.Board)

		api.GET("/actors", h.ListActors)
		api.POST("/actors", h.CreateActor)
		api.GET("/roles", h.ListRoles)
		api.POST("/roles", h.CreateRole)
		api.GET("/scenes", h.ListScenes)
		api.POST("/scenes", h.CreateScene)

		api.GET("/costumes", h.ListCostumes)
		api.POST("/costumes", h.CreateCostume)

		api.POST("/tasks/check", h.CheckTask)
		api.POST("/tasks", h.CreateTask)
		api.PUT("/tasks/:id", h.UpdateTask)
		api.DELETE("/tasks/:id", h.CancelTask)
		api.POST("/recast", h.Recast)

		api.POST("/scan/sign", h.ScanSign)
		api.POST("/scan/return", h.ScanReturn)

		api.GET("/care", h.ListCare)
		api.POST("/care", h.StartCare)
		api.POST("/care/:id/finish", h.FinishCare)

		api.GET("/issues", h.ListIssues)
		api.POST("/issues", h.ReportIssue)
		api.POST("/issues/:id/resolve", h.ResolveIssue)

		api.POST("/checks", h.StartCheck)
		api.GET("/checks/:id", h.GetCheck)
		api.POST("/checks/:id/items", h.RecordCheck)
	}
}

func ok(c *gin.Context, v any) { c.JSON(http.StatusOK, gin.H{"ok": true, "data": v}) }
func fail(c *gin.Context, code int, msg string, conflicts ...any) {
	m := gin.H{"ok": false, "error": msg}
	if len(conflicts) > 0 {
		m["conflicts"] = conflicts[0]
	}
	c.JSON(code, m)
}

// ---------- 基础数据 ----------

func (h *Handler) Seed(c *gin.Context) {
	if err := h.Svc.ResetAndSeed(); err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, gin.H{"seeded": true})
}

func (h *Handler) ListShows(c *gin.Context) {
	var shows []models.Show
	h.Svc.DB.Order("id desc").Find(&shows)
	ok(c, shows)
}

func (h *Handler) CreateShow(c *gin.Context) {
	var in models.Show
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if in.Status == "" {
		in.Status = "planned"
	}
	if in.TransferSeconds == 0 {
		in.TransferSeconds = 120
	}
	if in.CleaningSeconds == 0 {
		in.CleaningSeconds = 900
	}
	if err := h.Svc.DB.Create(&in).Error; err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, in)
}

func (h *Handler) Board(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	b, err := h.Svc.GetBoard(uint(id))
	if err != nil {
		fail(c, 404, err.Error())
		return
	}
	ok(c, b)
}

func (h *Handler) ListActors(c *gin.Context) {
	var v []models.Actor
	h.Svc.DB.Order("id asc").Find(&v)
	ok(c, v)
}
func (h *Handler) CreateActor(c *gin.Context) {
	var in models.Actor
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		fail(c, 400, "请填写演员姓名")
		return
	}
	h.Svc.DB.Create(&in)
	ok(c, in)
}

func (h *Handler) ListRoles(c *gin.Context) {
	q := h.Svc.DB.Order("id asc")
	if sid := c.Query("showId"); sid != "" {
		q = q.Where("show_id = ?", sid)
	}
	var v []models.Role
	q.Find(&v)
	ok(c, v)
}
func (h *Handler) CreateRole(c *gin.Context) {
	var in models.Role
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.ShowID == 0 {
		fail(c, 400, "请填写剧目与角色名")
		return
	}
	h.Svc.DB.Create(&in)
	ok(c, in)
}

func (h *Handler) ListScenes(c *gin.Context) {
	q := h.Svc.DB.Order("seq asc")
	if sid := c.Query("showId"); sid != "" {
		q = q.Where("show_id = ?", sid)
	}
	var v []models.Scene
	q.Find(&v)
	ok(c, v)
}
func (h *Handler) CreateScene(c *gin.Context) {
	var in models.Scene
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.ShowID == 0 {
		fail(c, 400, "请填写剧目与场次名")
		return
	}
	h.Svc.DB.Create(&in)
	ok(c, in)
}

func (h *Handler) ListCostumes(c *gin.Context) {
	var v []models.Costume
	h.Svc.DB.Order("code asc").Find(&v)
	ok(c, v)
}
func (h *Handler) CreateCostume(c *gin.Context) {
	var in models.Costume
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if in.Code == "" || in.Name == "" {
		fail(c, 400, "实物编号和名称必填")
		return
	}
	if in.Status == "" {
		in.Status = models.CostumeAvailable
	}
	if in.Location == "" {
		in.Location = "服装间"
	}
	if err := h.Svc.DB.Create(&in).Error; err != nil {
		fail(c, 400, "登记失败："+err.Error())
		return
	}
	ok(c, in)
}

// ---------- 换装任务 ----------

func (h *Handler) CheckTask(c *gin.Context) {
	var in service.TaskInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	cs, err := h.Svc.CheckTask(in)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"conflicts": cs})
}

type taskReq struct {
	service.TaskInput
	Force bool `json:"force"`
}

func (h *Handler) CreateTask(c *gin.Context) {
	var in taskReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	t, cs, err := h.Svc.CreateTask(in.TaskInput, in.Force)
	if err != nil {
		fail(c, 409, err.Error(), cs)
		return
	}
	ok(c, gin.H{"task": t, "conflicts": cs})
}

func (h *Handler) UpdateTask(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in taskReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	t, cs, err := h.Svc.UpdateTask(uint(id), in.TaskInput, in.Force)
	if err != nil {
		fail(c, 409, err.Error(), cs)
		return
	}
	ok(c, gin.H{"task": t, "conflicts": cs})
}

func (h *Handler) CancelTask(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.Svc.DB.Model(&models.Task{}).Where("id = ? AND status = ?", id, models.TaskPending).
		Update("status", models.TaskCancelled).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

type recastReq struct {
	ShowID     uint `json:"showId"`
	RoleID     uint `json:"roleId"`
	NewActorID uint `json:"newActorId"`
}

func (h *Handler) Recast(c *gin.Context) {
	var in recastReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	changed, blocked, err := h.Svc.Recast(in.ShowID, in.RoleID, in.NewActorID)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"changed": changed, "blocked": blocked})
}

// ---------- 模拟扫码 ----------

type signReq struct {
	TaskID  uint   `json:"taskId"`
	ActorID uint   `json:"actorId"`
	Code    string `json:"code"`
}

func (h *Handler) ScanSign(c *gin.Context) {
	var in signReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if err := h.Svc.ScanSign(in.TaskID, in.ActorID, in.Code); err != nil {
		fail(c, 409, err.Error())
		return
	}
	ok(c, gin.H{"signed": true})
}

type returnReq struct {
	TaskID uint   `json:"taskId"`
	Code   string `json:"code"`
}

func (h *Handler) ScanReturn(c *gin.Context) {
	var in returnReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if err := h.Svc.ScanReturn(in.TaskID, in.Code); err != nil {
		fail(c, 409, err.Error())
		return
	}
	ok(c, gin.H{"returned": true})
}

// ---------- 洗护/维修 ----------

func (h *Handler) ListCare(c *gin.Context) {
	q := h.Svc.DB.Order("id desc")
	if cid := c.Query("costumeId"); cid != "" {
		q = q.Where("costume_id = ?", cid)
	}
	var v []models.CareRecord
	q.Find(&v)
	ok(c, v)
}

type careReq struct {
	CostumeID uint   `json:"costumeId"`
	Type      string `json:"type"`
	Note      string `json:"note"`
}

func (h *Handler) StartCare(c *gin.Context) {
	var in careReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	rec, err := h.Svc.StartCare(in.CostumeID, in.Type, in.Note)
	if err != nil {
		fail(c, 409, err.Error())
		return
	}
	ok(c, rec)
}

func (h *Handler) FinishCare(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.Svc.FinishCare(uint(id)); err != nil {
		fail(c, 409, err.Error())
		return
	}
	ok(c, gin.H{"finished": true})
}

// ---------- 问题单 ----------

func (h *Handler) ListIssues(c *gin.Context) {
	q := h.Svc.DB.Order("id desc")
	if cid := c.Query("costumeId"); cid != "" {
		q = q.Where("costume_id = ?", cid)
	}
	if st := c.Query("status"); st != "" {
		q = q.Where("status = ?", st)
	}
	var v []models.Issue
	q.Find(&v)
	ok(c, v)
}

type issueReq struct {
	CostumeID uint   `json:"costumeId"`
	ShowID    *uint  `json:"showId"`
	Type      string `json:"type"`
	Note      string `json:"note"`
}

func (h *Handler) ReportIssue(c *gin.Context) {
	var in issueReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	issue, err := h.Svc.ReportIssue(in.CostumeID, in.ShowID, in.Type, in.Note)
	if err != nil {
		fail(c, 409, err.Error())
		return
	}
	ok(c, issue)
}

type resolveReq struct {
	Resolution string `json:"resolution"`
}

func (h *Handler) ResolveIssue(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in resolveReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if err := h.Svc.ResolveIssue(uint(id), in.Resolution); err != nil {
		fail(c, 409, err.Error())
		return
	}
	ok(c, gin.H{"resolved": true})
}

// ---------- 清点 ----------

func (h *Handler) StartCheck(c *gin.Context) {
	var in struct {
		ShowID uint `json:"showId"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	dto, err := h.Svc.StartCheck(in.ShowID)
	if err != nil {
		fail(c, 409, err.Error())
		return
	}
	ok(c, dto)
}

func (h *Handler) GetCheck(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	dto, err := h.Svc.GetCheck(uint(id))
	if err != nil {
		fail(c, 404, err.Error())
		return
	}
	ok(c, dto)
}

type checkItemReq struct {
	CostumeID uint   `json:"costumeId"`
	State     string `json:"state"`
	Note      string `json:"note"`
}

func (h *Handler) RecordCheck(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in checkItemReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if err := h.Svc.RecordCheck(uint(id), in.CostumeID, in.State, in.Note); err != nil {
		fail(c, 409, err.Error())
		return
	}
	ok(c, gin.H{"recorded": true})
}
