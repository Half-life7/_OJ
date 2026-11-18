package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"oj/internal/auth"
	"oj/internal/config"
	"oj/internal/judge"
	"oj/internal/judge/sandbox"
	"oj/internal/server"
	"oj/internal/store"
	"oj/seed"
)

func main() {
	dev := flag.Bool("dev", false, "从磁盘 web/ 加载前端资源")
	flag.Parse()
	c, e := config.Load()
	if e != nil {
		log.Fatal(e)
	}
	c.Dev = c.Dev || *dev
	s, e := store.Open(c.DBPath)
	if e != nil {
		log.Fatal("数据库初始化失败：", e)
	}
	defer s.Close()
	a, e := auth.New(s, c)
	if e != nil {
		log.Fatal("认证初始化失败：", e)
	}
	if e = a.EnsureAdmin(context.Background()); e != nil {
		log.Fatal("管理员初始化失败：", e)
	}
	if e = seed.Import(context.Background(), s); e != nil {
		log.Fatal("示例数据初始化失败：", e)
	}
	langs, e := judge.LoadLanguagesOrDefault(c.LanguagesFile)
	if e != nil {
		log.Fatal("语言配置加载失败：", e)
	}
	var sb sandbox.Sandbox = sandbox.NewGoJudge(c.SandboxURL, c.SandboxToken)
	if c.Sandbox == "isolate" {
		sb = sandbox.NewIsolate()
	}
	j := judge.New(s, sb, langs, c)
	if e = j.Start(context.Background()); e != nil {
		log.Fatal("判题队列初始化失败：", e)
	}
	srv := &http.Server{Addr: ":" + c.Port, Handler: server.New(s, a, j, c), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-stop
		log.Print("停止接收请求，等待当前判题结束…")
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = j.Close(ctx)
	}()
	log.Printf("OJ 已启动：http://localhost:%s（开发模式=%v）", c.Port, c.Dev)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
	<-done
}
