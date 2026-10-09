package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gatehouse/internal/gateway"
	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	data := flag.String("data", "./data", "数据目录")
	config := flag.String("config", "", "配置目录；留空沿用数据目录")
	logsDir := flag.String("log", "", "日志目录；留空沿用数据目录下的 logs")
	adminAddr := flag.String("admin", "0.0.0.0:16666", "管理界面监听地址")
	version := flag.Bool("version", false, "显示版本")
	supervise := flag.Bool("supervise", false, "启动维护管理程序")
	managedRoot := flag.String("managed-root", "", "可更新的程序目录")
	flag.Parse()
	if *version {
		fmt.Println(gateway.Version)
		return nil
	}
	paths, err := gateway.PrepareStorage(*data, *config, *logsDir)
	if err != nil {
		return errors.New("持久化目录准备或旧数据迁移失败：" + err.Error())
	}
	if *supervise && flag.NArg() == 0 {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return gateway.SupervisePaths(ctx, paths, *managedRoot, *adminAddr)
	}
	store, err := gateway.OpenStorePaths(paths)
	if err != nil {
		return errors.New("无法打开数据目录或配置文件，请检查权限和配置格式")
	}
	if flag.NArg() > 0 {
		if flag.NArg() == 1 && flag.Arg(0) == "migrate" {
			return nil
		}
		if flag.Arg(0) != "init" {
			return errors.New("支持的命令：init（设置或重置管理员账号密码）、migrate（迁移旧目录）；参数须放在命令前")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("请在交互终端运行 init，Docker 使用 docker compose run --rm gatehouse init")
		}
		fmt.Print("管理员账号（留空保留当前账号，首次默认 admin）：")
		username, err := readAdminUsername(os.Stdin, store.Snapshot().AdminUsername)
		if err != nil {
			return err
		}
		fmt.Print("管理密码（12–72 字节，输入不回显）: ")
		first, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return errors.New("密码读取失败")
		}
		fmt.Print("再次输入密码: ")
		second, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return errors.New("密码读取失败")
		}
		if string(first) != string(second) {
			return errors.New("两次密码不一致")
		}
		hash, err := gateway.HashPassword(first)
		clear(first)
		clear(second)
		if err != nil {
			return err
		}
		if err := store.SetAdminAccount(username, hash); err != nil {
			return err
		}
		fmt.Println("管理员账号密码已保存；如果服务正在运行，请重启服务。")
		return nil
	}
	if store.Snapshot().PasswordHash == "" {
		return errors.New("尚未初始化，请先运行 gatehouse -data <数据目录> init 设置管理员账号密码")
	}
	c := store.Snapshot().Config
	_, port, err := net.SplitHostPort(*adminAddr)
	if err != nil {
		return errors.New("管理监听地址格式应为 IP:端口")
	}
	adminPort, err := strconv.Atoi(port)
	if err != nil || adminPort < 1 || adminPort > 65535 {
		return errors.New("管理端口必须是 1–65535 的数字")
	}
	if err := validateConfigAdminPort(c, adminPort); err != nil {
		return err
	}
	subscriptions, err := gateway.NewSubscriptions(*data, store)
	if err != nil {
		return errors.New("无法打开订阅缓存目录")
	}
	proxy := gateway.NewProxy(c, subscriptions)
	if err := proxy.ConfigureState(store.Snapshot()); err != nil {
		return err
	}
	certs, err := gateway.NewCertificates(*data, store)
	if err != nil {
		return errors.New("无法打开证书目录")
	}
	jobs := gateway.NewJobs(store, certs)
	logs, err := gateway.NewLogsAt(paths.Log, store)
	if err != nil {
		return errors.New("无法打开日志目录")
	}
	admin := gateway.NewAdmin(store, proxy, certs, jobs, adminPort)
	maintenance, err := gateway.NewMaintenancePaths(paths, *managedRoot)
	if err != nil {
		return errors.New("无法打开维护目录")
	}
	admin.SetMaintenance(maintenance)
	proxy.SetLogs(logs)
	subscriptions.SetLogs(logs)
	jobs.SetLogs(logs)
	certs.SetLogs(logs)
	admin.SetLogs(logs)
	addresses := []string{*adminAddr}
	handlers := []http.Handler{admin.Handler()}
	tlsGetters := []func(*tls.ClientHelloInfo) (*tls.Certificate, error){nil}
	for _, group := range c.Groups {
		if !group.Enabled {
			continue
		}
		if group.HTTPPort != 0 {
			addresses = append(addresses, ":"+strconv.Itoa(group.HTTPPort))
			handlers = append(handlers, proxy.Handler(group.ID))
			tlsGetters = append(tlsGetters, nil)
		}
		if group.HTTPSPort != 0 {
			addresses = append(addresses, ":"+strconv.Itoa(group.HTTPSPort))
			handlers = append(handlers, proxy.Handler(group.ID))
			tlsGetters = append(tlsGetters, certs.ForGroup(group.ID))
		}
	}
	var listeners []net.Listener
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()
	for _, addr := range addresses {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("无法监听 %s，请检查端口占用", addr)
		}
		listeners = append(listeners, listener)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	servers := []*http.Server{}
	errorsCh := make(chan error, len(listeners))
	for i, listener := range listeners {
		server := &http.Server{Handler: handlers[i], ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10, ErrorLog: log.New(io.Discard, "", 0)}
		servers = append(servers, server)
		if tlsGetters[i] != nil {
			server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: tlsGetters[i]}
			go func() { errorsCh <- server.ServeTLS(listener, "", "") }()
		} else {
			go func() { errorsCh <- server.Serve(listener) }()
		}
	}
	jobs.Start(ctx)
	subscriptions.Start(ctx)
	logs.Start(ctx)
	if *managedRoot != "" {
		if err := gateway.MarkServiceReady(maintenance); err != nil {
			return errors.New("启动检查状态写入失败")
		}
	}
	log.Printf("Gatehouse 已启动，管理地址 http://%s，%d 个业务监听端口", *adminAddr, len(listeners)-1)
	var serveErr error
	select {
	case <-ctx.Done():
	case <-maintenance.RestartSignal():
	case err := <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = errors.New("监听服务意外停止")
		}
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	for _, server := range servers {
		_ = server.Shutdown(shutdown)
	}
	if maintenance.Busy() {
		log.Print("维护操作已安排，退出当前进程以应用更新或恢复")
	}
	return serveErr
}

// Read only through this newline; buffering must not consume the password that
// the terminal password reader will subsequently read without echo.
func readAdminUsername(reader io.Reader, current string) (string, error) {
	var data []byte
	var b [1]byte
	for {
		if _, err := io.ReadFull(reader, b[:]); err != nil {
			return "", errors.New("管理员账号读取失败")
		}
		if b[0] == '\n' {
			break
		}
		data = append(data, b[0])
		if len(data) > 65 {
			return "", errors.New("管理员账号须为 1–64 字节")
		}
	}
	username := strings.TrimSuffix(string(data), "\r")
	if username == "" {
		username = current
	}
	if err := gateway.ValidateAdminUsername(username); err != nil {
		return "", err
	}
	return username, nil
}

func validateConfigAdminPort(c gateway.Config, adminPort int) error {
	for _, g := range c.Groups {
		if g.HTTPPort == adminPort || g.HTTPSPort == adminPort {
			return errors.New("管理端口不能与业务端口相同")
		}
	}
	return nil
}
