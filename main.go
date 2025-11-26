package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"

	"puter2api/internal/handler"
	"puter2api/internal/storage"

	"github.com/gin-gonic/gin"
)

//go:embed web/*
var webFS embed.FS

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./puter2api.db"
	}

	// 初始化数据库
	store, err := storage.New(dbPath)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer store.Close()

	// 创建处理器 - 从数据库获取 Token
	h := handler.NewHandler(store)
	th := handler.NewTokenHandler(store)

	// 设置路由
	r := gin.Default()

	// Claude API 兼容端点
	r.POST("/v1/messages", h.HandleMessages)

	// Token 管理 API
	api := r.Group("/api")
	{
		api.GET("/tokens", th.ListTokens)
		api.POST("/tokens", th.AddToken)
		api.DELETE("/tokens/:id", th.DeleteToken)
		api.PUT("/tokens/:id", th.UpdateTokenName)
		api.PUT("/tokens/:id/toggle", th.ToggleToken)
		api.POST("/tokens/:id/test", th.TestToken)
		api.POST("/tokens/test-all", th.TestAllTokens)
	}

	// 静态文件服务 (Web UI)
	webContent, _ := fs.Sub(webFS, "web")
	r.StaticFS("/ui", http.FS(webContent))

	// 根路径重定向到 UI
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/ui/")
	})

	// 启动服务
	log.Printf("服务启动在 :%s", port)
	log.Printf("Web UI: http://localhost:%s/ui/", port)
	r.Run(":" + port)
}
