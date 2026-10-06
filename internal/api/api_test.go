package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"theater-costume/internal/models"
	"theater-costume/internal/service"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRouter(t *testing.T) (*gorm.DB, uint, uint, uint) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(&models.Actor{}, &models.Costume{}, &models.Performance{},
		&models.Assignment{}, &models.CareRecord{}, &models.Incident{},
		&models.InventoryCheck{}, &models.RecastLog{})

	perf := models.Performance{Name: "测试场", ShowTime: time.Now().Add(24 * time.Hour), Status: "scheduled"}
	db.Create(&perf)
	costume := models.Costume{Code: "C9", Name: "待洗衣物", Status: models.CostumeReturned,
		ChangeSeconds: 120, CleanSeconds: 1800, CurrentLocation: "洗衣房待洗篮"}
	db.Create(&costume)
	actor := models.Actor{Name: "演员甲"}
	db.Create(&actor)
	return db, perf.ID, costume.ID, actor.ID
}

// TestHTTP_BlockUnwashedAssignment 端到端：未经洗护的衣服排单返回 409，
// 明细带 unwashed 冲突码。
func TestHTTP_BlockUnwashedAssignment(t *testing.T) {
	db, perfID, costumeID, actorID := setupRouter(t)
	r := NewRouter(service.New(db))

	body, _ := json.Marshal(map[string]any{
		"performance_id": perfID, "costume_id": costumeID, "actor_id": actorID,
		"sequence": 1, "handover_at": time.Now().Add(24 * time.Hour),
		"need_by_at": time.Now().Add(25 * time.Hour),
		"return_at":   time.Now().Add(26 * time.Hour),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/assignments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("未洗护排单应返回 409，得到 %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Details []struct {
			Code string `json:"code"`
		} `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range resp.Details {
		if d.Code == service.ConflictUnwashed {
			found = true
		}
	}
	if !found {
		t.Fatalf("409 明细应含 unwashed，得到 %s", w.Body.String())
	}
}

// TestHTTP_RecastSignedLocked 端到端：已签收任务换角返回 409。
func TestHTTP_RecastSignedLocked(t *testing.T) {
	db, perfID, costumeID, actorID := setupRouter(t)
	svc := service.New(db)

	// 把衣服洗护恢复可用，再排单、签收，然后尝试换角。
	cleaned := time.Now()
	db.Model(&models.Costume{}).Where("id = ?", costumeID).
		Updates(map[string]any{"status": models.CostumeAvailable, "last_cleaned_at": cleaned})
	a2 := models.Actor{Name: "替身"}
	db.Create(&a2)

	base := time.Now().Add(24 * time.Hour)
	as, _, err := svc.CreateAssignment(&service.AssignmentInput{
		PerformanceID: perfID, CostumeID: costumeID, ActorID: actorID, Sequence: 1,
		HandoverAt: base, NeedByAt: base.Add(5 * time.Minute), ReturnAt: base.Add(20 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Scan(&service.ScanInput{Code: "C9", Action: "sign"}); err != nil {
		t.Fatalf("签收失败: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"actor_id": a2.ID, "reason": "临时换角"})
	req := httptest.NewRequest(http.MethodPost, "/api/assignments/"+itoaS(as.ID)+"/recast", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r := NewRouter(svc)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("已签收换角应 409，得到 %d: %s", w.Code, w.Body.String())
	}
}

func itoaS(u uint) string {
	return strconv.FormatUint(uint64(u), 10)
}
