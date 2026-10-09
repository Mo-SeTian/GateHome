// Package integration runs the vendored Sun-Panel in an isolated GateHome worker.
package integration

import (
	"context"
	"embed"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sun-panel/global"
	"sun-panel/initialize"
	"sun-panel/router"
	"syscall"
	"time"
)

//go:embed all:web
var frontend embed.FS

// Run uses a listener inherited from the parent, so no internal TCP port is exposed.
func Run(data string) error {
	syscall.Umask(0077)
	if err := os.Chdir(data); err != nil {
		return err
	}
	for _, dir := range []string{"custom", "uploads"} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	for _, name := range []string{"custom/index.css", "custom/index.js"} {
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			f.Close()
		} else if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	global.RUNCODE = "release"
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	if err := initialize.InitApp(); err != nil {
		return errors.New("Sun-Panel 初始化失败")
	}
	db, err := global.Db.DB()
	if err != nil {
		return err
	}
	defer db.Close()
	defer global.Logger.Sync()
	web, _ := fs.Sub(frontend, "web")
	handler := router.NewRouter(http.FS(web))
	listener, err := net.FileListener(os.NewFile(3, "sunpanel-listener"))
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
		return nil
	case err := <-done:
		return err
	}
}
