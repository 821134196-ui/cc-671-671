package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"theater-costumes/internal/api"
	"theater-costumes/internal/service"
	"theater-costumes/internal/store"
)

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data/theater.db"
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatal(err)
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	svc := service.New(db)
	h := api.New(svc)

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	h.Register(r)

	// 首次启动自动种子；前端静态文件
	var count int64
	db.Table("shows").Count(&count)
	if count == 0 {
		if err := svc.ResetAndSeed(); err != nil {
			log.Printf("seed: %v", err)
		} else {
			log.Println("已写入演示数据（POST /api/seed 可重置）")
		}
	}

	webDir := "web"
	if _, err := os.Stat(filepath.Join(webDir, "index.html")); err == nil {
		r.Static("/vendor", filepath.Join(webDir, "vendor"))
		r.StaticFile("/app.js", filepath.Join(webDir, "app.js"))
		r.StaticFile("/", filepath.Join(webDir, "index.html"))
		r.NoRoute(func(c *gin.Context) {
			if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
				c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "not found"})
				return
			}
			c.File(filepath.Join(webDir, "index.html"))
		})
	}

	log.Printf("剧院服装换装路线与场次清点系统启动: http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
