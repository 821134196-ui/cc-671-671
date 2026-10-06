package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"theater-costumes/internal/models"
	"theater-costumes/internal/service"
)

func setupRouter(t *testing.T) (*gin.Engine, *service.Service) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(models.AllModels()...)
	t.Cleanup(func() {
		for _, m := range models.AllModels() {
			db.Where("1=1").Delete(m)
		}
	})
	svc := service.New(db)
	if err := svc.ResetAndSeed(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	New(svc).Register(r)
	return r, svc
}

func do(t *testing.T, r *gin.Engine, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func mustOK(t *testing.T, code int, out map[string]any) {
	t.Helper()
	if code != 200 || out["ok"] != true {
		t.Fatalf("期望成功，实际 code=%d body=%v", code, out)
	}
}

func okDo(t *testing.T, r *gin.Engine, method, path string, body any) {
	t.Helper()
	code, out := do(t, r, method, path, body)
	mustOK(t, code, out)
}

// 端到端：未洗护再次分配被拦截 + 完成洗护后放行
func TestHTTPCareGate(t *testing.T) {
	r, svc := setupRouter(t)

	// 取种子里的演出与 C-003（使者斗篷）
	b, err := svc.GetBoard(1)
	if err != nil {
		t.Fatal(err)
	}
	var costumeID uint
	for _, c := range b.Costumes {
		if c.Code == "C-003" {
			costumeID = c.ID
		}
	}
	// 找到 C-003 的待签收任务并签收、交回
	var taskID uint
	for _, tk := range b.Tasks {
		if tk.CostumeCode == "C-003" && tk.Status == "pending" {
			taskID = tk.ID
		}
	}
	okDo(t, r, "POST", "/api/scan/sign", map[string]any{"taskId": taskID, "code": "C-003"})
	okDo(t, r, "POST", "/api/scan/return", map[string]any{"taskId": taskID, "code": "C-003"})
	// 送洗护
	code, out := do(t, r, "POST", "/api/care", map[string]any{"costumeId": costumeID, "type": "laundry", "note": "清洗"})
	mustOK(t, code, out)
	careID := uint(out["data"].(map[string]any)["id"].(float64))

	// 洗护未完成再安排同一件戏服 -> 409 且返回 costume_blocked 冲突
	code, out = do(t, r, "POST", "/api/tasks", map[string]any{
		"showId": 1, "costumeId": costumeID, "roleId": 3, "actorId": 2,
		"onAt": "2026-10-06T22:30", "offAt": "2026-10-06T22:50", "changePoint": "侧台左",
	})
	if code != 409 {
		t.Fatalf("期望 409 拦截，实际 %d body=%v", code, out)
	}
	cs, _ := out["conflicts"].([]any)
	found := false
	for _, c := range cs {
		cm := c.(map[string]any)
		if cm["type"] == "costume_blocked" {
			found = true
		}
	}
	if !found {
		t.Fatalf("冲突列表应包含 costume_blocked: %v", out)
	}

	// 错误条码签收被拒绝
	code, _ = do(t, r, "POST", "/api/scan/sign", map[string]any{"taskId": 1, "code": "WRONG-CODE"})
	if code != 409 {
		t.Fatalf("错误条码应返回 409，实际 %d", code)
	}

	// 完成洗护后可再次安排
	okDo(t, r, "POST", "/api/care/"+itoa(careID)+"/finish", nil)
	code, out = do(t, r, "POST", "/api/tasks", map[string]any{
		"showId": 1, "costumeId": costumeID, "roleId": 3, "actorId": 2,
		"onAt": "2026-10-07T22:30", "offAt": "2026-10-07T22:50", "changePoint": "侧台左",
	})
	mustOK(t, code, out)
}

// 端到端：临时换角只影响未签收任务
func TestHTTPRecast(t *testing.T) {
	r, _ := setupRouter(t)

	// 角色2（公主，演员2=苏晴）：先签收它的任务，再补排一条未签收任务
	code, out := do(t, r, "GET", "/api/shows/1/board", nil)
	mustOK(t, code, out)
	data := out["data"].(map[string]any)
	tasks := data["tasks"].([]any)
	var princessTask map[string]any
	for _, tk := range tasks {
		m := tk.(map[string]any)
		if m["roleName"] == "公主" {
			princessTask = m
		}
	}
	tid := uint(princessTask["id"].(float64))
	costumeCode := princessTask["costumeCode"].(string)
	okDo(t, r, "POST", "/api/scan/sign",
		map[string]any{"taskId": tid, "code": costumeCode})

	// 给公主再排一条未签收任务（换一件戏服避免与其它路线硬冲突——用 C-006 王子常服仅作测试载体；
	// 更稳妥：新增一件戏服）
	code, out = do(t, r, "POST", "/api/costumes", map[string]any{
		"code": "T-999", "name": "测试披风", "changeSeconds": 120,
		"status": "available", "location": "服装间",
	})
	mustOK(t, code, out)
	newCostumeID := uint(out["data"].(map[string]any)["id"].(float64))
	code, out = do(t, r, "POST", "/api/tasks", map[string]any{
		"showId": 1, "costumeId": newCostumeID, "roleId": 2, "actorId": 2,
		"onAt": "2026-10-07T20:00", "offAt": "2026-10-07T20:20", "changePoint": "化妆间",
	})
	mustOK(t, code, out)

	// 临时换角：公主 -> 演员4（陈骁）
	code, out = do(t, r, "POST", "/api/recast", map[string]any{
		"showId": 1, "roleId": 2, "newActorId": 4,
	})
	mustOK(t, code, out)
	changed := int(out["data"].(map[string]any)["changed"].(float64))
	if changed != 1 {
		t.Fatalf("应只换 1 条未签收任务，实际 %d", changed)
	}

	code, out = do(t, r, "GET", "/api/shows/1/board", nil)
	mustOK(t, code, out)
	tasks = out["data"].(map[string]any)["tasks"].([]any)
	for _, tk := range tasks {
		m := tk.(map[string]any)
		if uint(m["id"].(float64)) == tid {
			if m["actorName"] != "苏晴" || m["signedActor"] != "苏晴" {
				t.Fatalf("已签收任务必须仍关联原演员，实际 actor=%v signed=%v", m["actorName"], m["signedActor"])
			}
		}
		if m["costumeCode"] == "T-999" {
			if m["actorName"] != "陈骁" {
				t.Fatalf("未签收任务应换成陈骁，实际 %v", m["actorName"])
			}
			if m["originalActor"] != "苏晴" {
				t.Fatalf("应保留原演员记录，实际 %v", m["originalActor"])
			}
		}
	}
}

// 端到端：清点少件 -> 生成问题单 -> 分配拦截 -> 结单后放行
func TestHTTPCheckIssueFlow(t *testing.T) {
	r, _ := setupRouter(t)

	code, out := do(t, r, "POST", "/api/checks", map[string]any{"showId": 1})
	mustOK(t, code, out)
	session := out["data"].(map[string]any)
	sid := uint(session["session"].(map[string]any)["id"].(float64))
	var costumeID uint
	for _, it := range session["items"].([]any) {
		m := it.(map[string]any)
		if m["costumeCode"] == "C-005" {
			costumeID = uint(m["costumeId"].(float64))
		}
	}
	okDo(t, r, "POST", "/api/checks/"+itoa(sid)+"/items",
		map[string]any{"costumeId": costumeID, "state": "missing", "note": "散场未找到"})

	// 问题单自动生成
	code, out = do(t, r, "GET", "/api/issues?status=open", nil)
	mustOK(t, code, out)
	var issueID uint
	for _, v := range out["data"].([]any) {
		m := v.(map[string]any)
		if m["type"] == "missing" {
			issueID = uint(m["id"].(float64))
		}
	}
	if issueID == 0 {
		t.Fatal("应自动生成少件问题单")
	}

	// 分配被拦截
	code, out = do(t, r, "POST", "/api/tasks", map[string]any{
		"showId": 1, "costumeId": costumeID, "roleId": 1, "actorId": 1,
		"onAt": "2026-10-08T20:00", "offAt": "2026-10-08T20:20", "changePoint": "侧台左",
	})
	if code != 409 {
		t.Fatalf("有未处理问题单时应 409，实际 %d", code)
	}

	// 空处理说明不能结单
	code, _ = do(t, r, "POST", "/api/issues/"+itoa(issueID)+"/resolve", map[string]any{"resolution": ""})
	if code != 409 {
		t.Fatalf("空处理说明应被拒绝，实际 %d", code)
	}
	okDo(t, r, "POST", "/api/issues/"+itoa(issueID)+"/resolve",
		map[string]any{"resolution": "在侧台右道具箱找回，检查无损"})

	// 结单后可安排
	code, out = do(t, r, "POST", "/api/tasks", map[string]any{
		"showId": 1, "costumeId": costumeID, "roleId": 1, "actorId": 1,
		"onAt": "2026-10-08T20:00", "offAt": "2026-10-08T20:20", "changePoint": "侧台左",
	})
	mustOK(t, code, out)
}

func itoa(u uint) string {
	return strconv.FormatUint(uint64(u), 10)
}
