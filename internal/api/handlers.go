package api

import (
	"net/http"
	"strconv"

	"theater-costume/internal/models"
	"theater-costume/internal/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Svc *service.Service
}

func NewRouter(svc *service.Service) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	h := &Handler{Svc: svc}

	api := r.Group("/api")
	{
		api.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

		api.GET("/actors", h.listActors)
		api.POST("/actors", h.createActor)

		api.GET("/costumes", h.listCostumes)
		api.POST("/costumes", h.createCostume)
		api.GET("/costumes/:id", h.getCostume)
		api.GET("/costumes/:id/care", h.listCare)
		api.POST("/costumes/:id/care", h.startCare)
		api.POST("/care/:id/complete", h.completeCare)

		api.GET("/performances", h.listPerformances)
		api.POST("/performances", h.createPerformance)
		api.GET("/performances/:id", h.getPerformance)

		api.POST("/assignments", h.createAssignment)
		api.POST("/assignments/:id/recast", h.recast)

		// 扫码接口（本地模拟扫码枪）。
		api.POST("/scan", h.scan)

		// 散场清点 + 少件/破损事件。
		api.POST("/performances/:id/check", h.checkOne)
		api.GET("/performances/:id/checks", h.listChecks)
		api.GET("/incidents", h.listIncidents)
		api.POST("/incidents/:id/resolve", h.resolveIncident)
	}
	return r
}

func fail(c *gin.Context, code int, msg string, data any) {
	c.JSON(code, gin.H{"error": msg, "details": data})
}

func handleErr(c *gin.Context, err error) {
	if ce, ok := service.IsConflict(err); ok {
		fail(c, http.StatusConflict, ce.Error(), ce.Conflicts)
		return
	}
	if err == service.ErrNotFound {
		fail(c, http.StatusNotFound, err.Error(), nil)
		return
	}
	fail(c, http.StatusBadRequest, err.Error(), nil)
}

func atoi(s string) uint {
	n, _ := strconv.ParseUint(s, 10, 64)
	return uint(n)
}

func (h *Handler) listActors(c *gin.Context) {
	out, err := h.Svc.ListActors()
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, out)
}

func (h *Handler) createActor(c *gin.Context) {
	var a models.Actor
	if err := c.ShouldBindJSON(&a); err != nil {
		handleErr(c, err)
		return
	}
	if err := h.Svc.CreateActor(&a); err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(201, a)
}

func (h *Handler) listCostumes(c *gin.Context) {
	out, err := h.Svc.ListCostumes()
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, out)
}

func (h *Handler) createCostume(c *gin.Context) {
	var co models.Costume
	if err := c.ShouldBindJSON(&co); err != nil {
		handleErr(c, err)
		return
	}
	if err := h.Svc.CreateCostume(&co); err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(201, co)
}

func (h *Handler) getCostume(c *gin.Context) {
	co, err := h.Svc.GetCostume(atoi(c.Param("id")))
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, co)
}

func (h *Handler) listCare(c *gin.Context) {
	out, err := h.Svc.ListCare(atoi(c.Param("id")))
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, out)
}

type startCareReq struct {
	Type     string `json:"type"`
	Reason   string `json:"reason"`
	Operator string `json:"operator"`
}

func (h *Handler) startCare(c *gin.Context) {
	var in startCareReq
	if err := c.ShouldBindJSON(&in); err != nil {
		handleErr(c, err)
		return
	}
	rec, err := h.Svc.StartCare(atoi(c.Param("id")), &service.StartCareInput{
		Type: in.Type, Reason: in.Reason, Operator: in.Operator,
	})
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(201, rec)
}

func (h *Handler) completeCare(c *gin.Context) {
	var in struct {
		Operator string `json:"operator"`
	}
	_ = c.ShouldBindJSON(&in)
	co, err := h.Svc.CompleteCare(atoi(c.Param("id")), in.Operator)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, gin.H{"costume": co, "message": "洗护/维修完成，服装已恢复可用"})
}

func (h *Handler) listPerformances(c *gin.Context) {
	out, err := h.Svc.ListPerformances()
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, out)
}

func (h *Handler) createPerformance(c *gin.Context) {
	var p models.Performance
	if err := c.ShouldBindJSON(&p); err != nil {
		handleErr(c, err)
		return
	}
	if err := h.Svc.CreatePerformance(&p); err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(201, p)
}

func (h *Handler) getPerformance(c *gin.Context) {
	d, err := h.Svc.GetPerformance(atoi(c.Param("id")))
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, d)
}

func (h *Handler) createAssignment(c *gin.Context) {
	var in service.AssignmentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		handleErr(c, err)
		return
	}
	as, all, err := h.Svc.CreateAssignment(&in)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(201, gin.H{"assignment": as, "conflicts": all})
}

func (h *Handler) recast(c *gin.Context) {
	var in service.RecastInput
	if err := c.ShouldBindJSON(&in); err != nil {
		handleErr(c, err)
		return
	}
	as, cs, err := h.Svc.Recast(atoi(c.Param("id")), &in)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, gin.H{"assignment": as, "conflicts": cs})
}

func (h *Handler) scan(c *gin.Context) {
	var in service.ScanInput
	if err := c.ShouldBindJSON(&in); err != nil {
		handleErr(c, err)
		return
	}
	res, err := h.Svc.Scan(&in)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, res)
}

func (h *Handler) checkOne(c *gin.Context) {
	var in service.CheckItem
	if err := c.ShouldBindJSON(&in); err != nil {
		handleErr(c, err)
		return
	}
	res, err := h.Svc.CheckOne(atoi(c.Param("id")), &in)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(201, res)
}

func (h *Handler) listChecks(c *gin.Context) {
	out, err := h.Svc.ListChecks(atoi(c.Param("id")))
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, out)
}

func (h *Handler) listIncidents(c *gin.Context) {
	out, err := h.Svc.ListIncidents(c.Query("status"))
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, out)
}

func (h *Handler) resolveIncident(c *gin.Context) {
	var in service.ResolveIncidentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		handleErr(c, err)
		return
	}
	inc, co, err := h.Svc.ResolveIncident(atoi(c.Param("id")), &in)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(200, gin.H{"incident": inc, "costume": co})
}
