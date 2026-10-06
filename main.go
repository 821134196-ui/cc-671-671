package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"theater-costume/internal/api"
	"theater-costume/internal/service"
	"theater-costume/internal/store"

	"github.com/gin-gonic/gin"
)

//go:embed all:web/dist
var distFS embed.FS

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "监听地址")
	dbPath := flag.String("db", envOr("DB_PATH", "data/theater.db"), "SQLite 数据库路径")
	seed := flag.Bool("seed", true, "空库时写入演示数据")
	flag.Parse()

	if err := os.MkdirAll(dataDir(*dbPath), 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	if *seed {
		if err := store.Seed(db); err != nil {
			log.Fatalf("写入演示数据失败: %v", err)
		}
	}

	svc := service.New(db)
	r := api.NewRouter(svc)
	registerSPA(r)

	log.Printf("剧院服装换装系统已启动：http://localhost%s", *addr)
	if err := http.ListenAndServe(*addr, r); err != nil {
		log.Fatal(err)
	}
}

// registerSPA 托管内嵌的 Vue 构建产物；未命中静态文件时回退 index.html。
func registerSPA(r *gin.Engine) {
	sub, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	r.NoRoute(func(c *gin.Context) {
		// 未命中的 API 路径返回 404 JSON，而非 SPA 首页。
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/api" {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		p := strings.TrimPrefix(c.Request.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			// SPA 前端路由（如 /performances/1）回退。
			c.Request.URL.Path = "/"
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}

func dataDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
