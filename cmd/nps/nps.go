package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/acme"
	"ehang.io/nps/lib/daemon"
	"ehang.io/nps/server"
	"flag"
	"fmt"
	"github.com/fatih/color"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/install"
	"ehang.io/nps/lib/version"
	"ehang.io/nps/server/connection"
	"ehang.io/nps/server/tool"
	"ehang.io/nps/web/routers"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/web"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"

	"github.com/kardianos/service"
)

var (
	level      string
	ver        = flag.Bool("version", false, "show current version")
	confPath   = flag.String("conf_path", "", "set current confPath")
	serverCmd  = flag.Bool("server", false, "NPS管理脚本")
	npsLogPath = flag.String("log_path", "", "nps log path")
)

func main() {

	debug.SetMaxThreads(1000000)

	flag.Parse()
	// init log
	if *ver {
		common.PrintVersion()
		return
	}
	if *serverCmd {
		printSlogan()
		inputCmd()
		return
	}

	var logPath string
	// *confPath why get null value ?
	for _, v := range os.Args[1:] {
		switch v {
		case "install", "start", "stop", "uninstall", "restart":
			continue
		}
		if strings.Contains(v, "-conf_path=") {
			common.ConfPath = strings.Replace(v, "-conf_path=", "", -1)
		}

		if strings.Contains(v, "-log_path=") {
			logPath = strings.Replace(v, "-log_path=", "", -1)
		}
	}

	// auto-generate default config files if not exist
	initConfig(filepath.Join(common.GetRunPath(), "conf"))

	if err := beego.LoadAppConfig("ini", filepath.Join(common.GetRunPath(), "conf", "nps.conf")); err != nil {
		log.Fatalln("load config file error", err.Error())
	}

	common.InitPProfFromFile()
	if level = beego.AppConfig.String("log_level"); level == "" {
		level = "7"
	}
	logs.Reset()
	logs.EnableFuncCallDepth(true)
	logs.SetLogFuncCallDepth(3)

	if logPath == "" {
		logPath := beego.AppConfig.String("log_path")
		if logPath == "" {
			logPath = common.GetLogPath()
		}
		if common.IsWindows() {
			logPath = strings.Replace(logPath, "\\", "\\\\", -1)
		}
	}

	// init service
	options := make(service.KeyValue)
	svcConfig := &service.Config{
		Name:        "Nps",
		DisplayName: "nps内网穿透代理服务器",
		Description: "一款轻量级、功能强大的内网穿透代理服务器。支持tcp、udp流量转发，支持内网http代理、内网socks5代理，同时支持snappy压缩、站点保护、加密传输、多路复用、header修改等。支持web图形化管理，集成多用户模式。",
		Option:      options,
	}

	bridge.ServerTlsEnable = beego.AppConfig.DefaultBool("tls_enable", false)

	for _, v := range os.Args[1:] {
		switch v {
		case "install", "start", "stop", "uninstall", "restart":
			continue
		}
		svcConfig.Arguments = append(svcConfig.Arguments, v)
	}

	svcConfig.Arguments = append(svcConfig.Arguments, "service")
	if len(os.Args) > 1 && os.Args[1] == "service" {
		_ = logs.SetLogger(logs.AdapterFile, `{"level":`+level+`,"filename":"`+logPath+`","daily":false,"maxlines":100000,"color":true}`)
	} else {
		_ = logs.SetLogger(logs.AdapterConsole, `{"level":`+level+`,"color":true}`)
	}
	if !common.IsWindows() {
		svcConfig.Dependencies = []string{
			"Requires=network.target",
			"After=network-online.target syslog.target"}
		svcConfig.Option["SystemdScript"] = install.SystemdScript
		svcConfig.Option["SysvScript"] = install.SysvScript
	}
	prg := &nps{}
	prg.exit = make(chan struct{})
	s, err := service.New(prg, svcConfig)
	if err != nil {
		logs.Error(err, "service function disabled")
		run()
		// run without service
		wg := sync.WaitGroup{}
		wg.Add(1)
		wg.Wait()
		return
	}

	if len(os.Args) > 1 && os.Args[1] != "service" {
		switch os.Args[1] {
		case "reload":
			daemon.InitDaemon("nps", common.GetRunPath(), common.GetTmpPath())
			return
		case "install":
			// uninstall before
			_ = service.Control(s, "stop")
			_ = service.Control(s, "uninstall")

			binPath := install.InstallNps()
			svcConfig.Executable = binPath
			s, err := service.New(prg, svcConfig)
			if err != nil {
				logs.Error(err)
				return
			}
			err = service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				confPath := "/etc/init.d/" + svcConfig.Name
				os.Symlink(confPath, "/etc/rc.d/S90"+svcConfig.Name)
				os.Symlink(confPath, "/etc/rc.d/K02"+svcConfig.Name)
			}
			return
		case "start", "restart", "stop":
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				cmd := exec.Command("/etc/init.d/"+svcConfig.Name, os.Args[1])
				err := cmd.Run()
				if err != nil {
					logs.Error(err)
				}
				return
			}
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			return
		case "uninstall":
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				os.Remove("/etc/rc.d/S90" + svcConfig.Name)
				os.Remove("/etc/rc.d/K02" + svcConfig.Name)
			}
			return
		case "update":
			install.UpdateNps()
			return
			//default:
			//	logs.Error("command is not support")
			//	return
		}
	}

	_ = s.Run()
}

func printSlogan() {
	green := color.New(color.FgGreen).SprintFunc()
	// 第一次输入，如果输入 1,2,3，4 则需要输入秘钥，否则

	fmt.Printf("%s", green(""))

	fmt.Printf("\033[32;0m欢迎使用 NPS 管理脚本，当前版本：v%s\n", version.VERSION)
	fmt.Printf("\033[0m") // 重置颜色

	fmt.Printf("\n")

	fmt.Printf("\u001B[32m输入[1]\u001B[0m - 安装 NPS\n")
	fmt.Printf("\u001B[32m输入[2]\u001B[0m - 卸载 NPS\n")
	fmt.Printf("\u001B[32m输入[3]\u001B[0m - 更新 NPS\n")
	fmt.Printf("---------------------\n")
	fmt.Printf("\u001B[32m输入[4]\u001B[0m - 查看状态\n")
	fmt.Printf("---------------------\n")
	fmt.Printf("\u001B[32m输入[5]\u001B[0m - 启动 NPS\n")
	fmt.Printf("\u001B[32m输入[6]\u001B[0m - 停止 NPS\n")
	fmt.Printf("\u001B[32m输入[7]\u001B[0m - 重启 NPS\n")
	fmt.Printf("---------------------\n")
	fmt.Printf("\u001B[32m输入[0]\u001B[0m - 退出脚本\n")
	fmt.Printf("---------------------\n")
	fmt.Printf("\n")

}

func inputCmd() {
	var flag string
	fmt.Printf("请输入[0-7]：")

	stdin := bufio.NewReader(os.Stdin)
	_, err := fmt.Fscanln(stdin, &flag)
	if err != nil {
		fmt.Println("输入有误")
	} else {
		if flag == "0" {
			os.Exit(0)
		}

		// init service

		prg := &nps{
			exit: make(chan struct{}),
		}
		options := make(service.KeyValue)
		svcConfig := &service.Config{
			Name:        "Nps",
			DisplayName: "nps内网穿透代理服务器",
			Description: "一款轻量级、功能强大的内网穿透代理服务器。支持tcp、udp流量转发，支持内网http代理、内网socks5代理，同时支持snappy压缩、站点保护、加密传输、多路复用、header修改等。支持web图形化管理，集成多用户模式。",
			Option:      options,
		}
		s, _ := service.New(prg, svcConfig)

		switch flag {
		case "1":
			// uninstall before
			_ = service.Control(s, "stop")
			_ = service.Control(s, "uninstall")
			binPath := install.InstallNpsToCurrentDir()

			beego.LoadAppConfig("ini", filepath.Join(common.GetAppPath(), "conf", "nps.conf"))

			logPath := filepath.Join(common.GetAppPath(), "nps.log")
			if common.IsWindows() {
				logPath = strings.Replace(logPath, "\\", "\\\\", -1)
			}
			svcConfig.Arguments = append(svcConfig.Arguments, "service")
			svcConfig.Arguments = append(svcConfig.Arguments, "-conf_path="+common.GetAppPath())
			svcConfig.Arguments = append(svcConfig.Arguments, "-log_path="+logPath)

			fmt.Println("日志文件路径为：", logPath)

			svcConfig.Executable = binPath
			s, err := service.New(prg, svcConfig)

			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				confPath := "/etc/init.d/" + svcConfig.Name
				os.Symlink(confPath, "/etc/rc.d/S90"+svcConfig.Name)
				os.Symlink(confPath, "/etc/rc.d/K02"+svcConfig.Name)
			}

			err = service.Control(s, "install")
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			} else {
				fmt.Println("NPS服务安装成功")
			}

			err = service.Control(s, "start")
			if err != nil {
				fmt.Println("启动NPS服务失败", err)
			} else {
				fmt.Println("NPS服务已启动，管理面板访问地址：127.0.0.1:" + beego.AppConfig.String("web_port"))
			}

			break
		case "2":
			// 卸载系统服务
			err := service.Control(s, "stop")
			if err != nil {
				fmt.Println("NPS服务停止失败", err)
			} else {
				fmt.Println("NPS服务已停止")
			}

			err = service.Control(s, "uninstall")
			if err != nil {
				logs.Error("NPS服务卸载失败")
			}
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				os.Remove("/etc/rc.d/S90" + svcConfig.Name)
				os.Remove("/etc/rc.d/K02" + svcConfig.Name)
			}

			if err == nil {
				fmt.Println("NPS服务已卸载成功")
			}
			break
		case "3":
			install.UpdateNpsNew()
			break
		case "4":
			// 查看状态
			var statusMsg = ""
			status, err := s.Status()
			if err != nil {
				statusMsg = "\u001B[31m未运行\u001B[0m"
			} else {
				if status == 1 {
					statusMsg = "\u001B[32m运行中\u001B[0m"
				} else {
					statusMsg = "\u001B[31m未运行\u001B[0m"
				}
			}
			fmt.Println("NPS服务状态：" + statusMsg)
			break
		case "5":
			// 启动 NPS
			err := service.Control(s, "start")
			if err != nil {
				fmt.Println("NPS服务启动失败", err)
			} else {
				fmt.Println("NPS服务启动成功")
			}

			break
		case "6":
			// 停止 NPS
			err := service.Control(s, "stop")
			if err != nil {
				fmt.Println("NPS服务停止失败", err)
			} else {
				fmt.Println("NPS服务停止成功")
			}

			break
		case "7":
			// 重启 NPS
			err := service.Control(s, "restart")
			if err != nil {
				fmt.Println("NPS服务重启失败", err)
			} else {
				fmt.Println("NPS服务重启成功")
			}

			break
		}
	}

	inputCmd()
}

func installNps() {

}

type nps struct {
	exit chan struct{}
}

func (p *nps) Start(s service.Service) error {
	_, _ = s.Status()
	go p.run()
	return nil
}
func (p *nps) Stop(s service.Service) error {
	_, _ = s.Status()
	close(p.exit)
	if service.Interactive() {
		os.Exit(0)
	}
	return nil
}

func (p *nps) run() error {
	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			logs.Warning("nps: panic serving %v: %v\n%s", err, string(buf))
		}
	}()
	run()
	select {
	case <-p.exit:
		logs.Warning("stop...")
	}
	return nil
}

func run() {
	routers.Init()
	task := &file.Tunnel{
		Mode: "webServer",
	}
	bridgePort, err := beego.AppConfig.Int("bridge_port")
	if err != nil {
		logs.Error("Getting bridge_port error", err)
		os.Exit(0)
	}

	logs.Info("日志路径：" + *npsLogPath)
	logs.Info("the config path is:" + common.GetRunPath())
	logs.Info("the version of server is %s ,allow client core version to be %s,tls enable is %t", version.VERSION, version.GetVersion(), bridge.ServerTlsEnable)
	connection.InitConnectionService()
	//crypt.InitTls(filepath.Join(common.GetRunPath(), "conf", "server.pem"), filepath.Join(common.GetRunPath(), "conf", "server.key"))
	crypt.InitTls()
	tool.InitAllowPort()
	tool.StartSystemInfo()
	timeout, err := beego.AppConfig.Int("disconnect_timeout")
	if err != nil {
		timeout = 60
	}
	go server.StartNewServer(bridgePort, task, beego.AppConfig.String("bridge_type"), timeout)

	// 启动 ACME 自动证书管理后台续期 goroutine
	acmeCtx, acmeCancel := context.WithCancel(context.Background())
	defer acmeCancel()
	acme.GetManager().Init(acmeCtx)
}

func initConfig(confDir string) {
	if !common.FileExists(confDir) {
		os.MkdirAll(confDir, 0755)
	}
	confPath := filepath.Join(confDir, "nps.conf")
	if !common.FileExists(confPath) {
		webPassword := crypt.GetRandomString(8)
		authKey := crypt.GetRandomString(8)
		authCryptKey := crypt.GetRandomString(16)
		content := strings.Replace(defaultNpsConf, "web_password=123", "web_password="+webPassword, 1)
		content = strings.Replace(content, "auth_key=123", "auth_key="+authKey, 1)
		content = strings.Replace(content, "auth_crypt_key =213", "auth_crypt_key ="+authCryptKey, 1)
		f, err := os.Create(confPath)
		if err != nil {
			return
		}
		defer f.Close()
		f.WriteString(content)
		logs.Info("Auto-generated default config file:", confPath)
		logs.Info("Web login username: admin, password:", webPassword)
		logs.Info("auth_key:", authKey)
		logs.Info("auth_crypt_key:", authCryptKey)
	}
	// 无论新装/升级, 都走一遍 compat:
	//   - 新装: defaultNpsConf 里的 nps_master_key= 是空占位, 会被替换为新 32 字节 base64
	//   - 升级: 缺的字段会被补上, 已有非空字段不动
	// 这样 deriveMachineKey() 始终走 nps.conf 路径, 不会写到 .acme_master_key 文件,
	// 跨重启跨容器迁移密文都能解
	ensureAcmeConfFields(confPath)
	web.ExtractWebFiles(common.GetRunPath())
}

// ensureAcmeConfFields 老 nps.conf 升级兼容:
//   - acme_email 缺则追加 acme_email=admin@lemaer.xyz
//   - nps_master_key 缺(或值为空)则生成 32 字节随机 base64 写进 nps.conf
//   - 不会覆盖用户已有的非注释值
//   - 保留原文件换行符(LF / CRLF), 不引入无谓的 diff
//
// 注释行(以 ; 或 # 开头)不算"已配置" —— 用户主动注释的我们也照样补,
// 因为留着空 nps_master_key 启动时会在 .acme_master_key 路径写入随机 key,
// 跟 nps.conf 路径分裂容易让用户困惑。不如直接给个稳定固定值。
//
// 注释行以外, "key=" / "key =空值" 视为"需要补", "key=xxx" 才视为"已配置"。
// 空值行会被替换为带值的行(不重复出现, 避免 beego 解析时冲突)。
func ensureAcmeConfFields(confPath string) {
	raw, err := os.ReadFile(confPath)
	if err != nil {
		logs.Warn("compat: read nps.conf failed, skip acme field ensure: %v", err)
		return
	}
	// 检测原文件换行符(LF / CRLF), 保持原样
	eol := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		eol = "\r\n"
	}
	// 去掉末尾一个 eol(避免重复), 后面追加时再加
	text := string(raw)
	if strings.HasSuffix(text, eol) {
		text = text[:len(text)-len(eol)]
	}
	// 沿原 eol 切行(避免 CRLF 文件被切成 "key=\r" 然后 prefix 匹配失败)
	lines := strings.Split(text, eol)
	hasEmail, hasMaster := false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "acme_email=") || strings.HasPrefix(trimmed, "acme_email =") {
			v := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "acme_email="), "acme_email ="))
			if v != "" {
				hasEmail = true
			}
		}
		if strings.HasPrefix(trimmed, "nps_master_key=") || strings.HasPrefix(trimmed, "nps_master_key =") {
			v := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "nps_master_key="), "nps_master_key ="))
			if v != "" {
				hasMaster = true
			}
		}
	}
	if hasEmail && hasMaster {
		return
	}
	// 在文件末尾追加缺的字段(带注释引导, 用户能看出含义)
	// 但如果原文件已有 "key=" / "key =空值" 行, 就用新值**替换**那一行(不重复)
	var sb strings.Builder
	emailReplaced, masterReplaced := false, false
	autoKey := generateMasterKey32B()
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 跳过空值 acme_email= 行, 用新值替换
		if !hasEmail && !emailReplaced {
			if strings.HasPrefix(trimmed, "acme_email=") || strings.HasPrefix(trimmed, "acme_email =") {
				v := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "acme_email="), "acme_email ="))
				if v == "" {
					sb.WriteString("acme_email=admin@lemaer.xyz")
					sb.WriteString(eol)
					emailReplaced = true
					logs.Info("compat: nps.conf 有空 acme_email=, 已替换为 admin@lemaer.xyz(可手动改)")
					continue
				}
			}
		}
		// 跳过空值 nps_master_key= 行, 用新值替换
		if !hasMaster && !masterReplaced {
			if strings.HasPrefix(trimmed, "nps_master_key=") || strings.HasPrefix(trimmed, "nps_master_key =") {
				v := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "nps_master_key="), "nps_master_key ="))
				if v == "" {
					sb.WriteString("nps_master_key=" + autoKey)
					sb.WriteString(eol)
					masterReplaced = true
					logs.Info("compat: nps.conf 有空 nps_master_key=, 已替换为新生成的 32 字节随机 base64")
					continue
				}
			}
		}
		sb.WriteString(line)
		sb.WriteString(eol)
	}
	if !hasEmail && !emailReplaced {
		sb.WriteString(eol)
		sb.WriteString("; ACME 注册邮箱(由 compat 兼容层追加, v0.26.36 起, 可手动改成你自己的)" + eol)
		sb.WriteString("acme_email=admin@lemaer.xyz" + eol)
		logs.Info("compat: nps.conf 缺 acme_email, 已追加 acme_email=admin@lemaer.xyz")
	}
	if !hasMaster && !masterReplaced {
		sb.WriteString(eol)
		sb.WriteString("; ACME master key(由 compat 兼容层自动生成, v0.26.36 起)" + eol)
		sb.WriteString("; 32 字节随机 base64 字符串, 用于加密 DNS API Key Secret" + eol)
		sb.WriteString("; 推荐: 升级后立即手改成你自己的固定值(任何字符串都行, sha256 后做 AES-256 key)" + eol)
		sb.WriteString("; 警告: 修改这个值后, 旧加密的 Key Secret 全部解不开, 需要在 SSL 凭证页重新填一次" + eol)
		sb.WriteString("nps_master_key=" + autoKey + eol)
		logs.Info("compat: nps.conf 缺 nps_master_key, 已自动生成 32 字节随机 base64 写进去(可手动改成你自己的固定值)")
	}
	if err := os.WriteFile(confPath, []byte(sb.String()), 0644); err != nil {
		logs.Warn("compat: 写 nps.conf 失败, acme_email/nps_master_key 没补上: %v", err)
	}
}

// generateMasterKey32B 生成 32 字节加密随机数 → 标准 base64 字符串(44 字符)。
// nps_master_key 内部 sha256 后做 AES-256 key, 任意非空字符串都行, 给个 32 字节 base64
// 是为了足够随机 + 用户改时知道是 base64 格式。
func generateMasterKey32B() string {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		// 极少见(熵不足), 兜底用 GetRandomString(64) 给个看着像 base64 的字符串
		logs.Warn("compat: 没法用 crypto/rand 生成 32 字节, 走 fallback: %v", err)
		return crypt.GetRandomString(64)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

const defaultNpsConf = `http_proxy_ip=0.0.0.0
http_proxy_port=80
https_proxy_port=443
show_http_proxy_port=true

bridge_type=tcp
bridge_port=8024
bridge_ip=0.0.0.0

public_vkey=123

flow_store_interval=1

log_level=6
log_path=nps.log

web_host=a.o.com
web_username=admin
web_password=123
web_port = 8081
web_ip=0.0.0.0
web_base_url=
web_open_ssl=false
web_cert_file=conf/server.pem
web_key_file=conf/server.key

auth_key=123
auth_crypt_key =213

allow_user_login=true
allow_user_register=false
allow_user_change_username=true

allow_flow_limit=true
allow_rate_limit=true
allow_tunnel_num_limit=true
allow_local_proxy=false
allow_connection_num_limit=true
allow_multi_ip=true
system_info_display=true

http_add_origin_header=true

http_cache=false
http_cache_length=100

disconnect_timeout=60

open_captcha=false

tls_enable=true
tls_bridge_port=8025

; ACME / Let's Encrypt 自动证书相关
; acme_email 必填: Let's Encrypt 注册账号的邮箱(没填证书申请会失败)
acme_email=admin@lemaer.xyz
; nps_master_key 选填: 用于加密 DNS API Key Secret 的 32 字节 base64 字符串
; 这里留空的话, ensureAcmeConfFields() 会在首次启动时自动生成 32 字节随机 base64 写进这一行
; 显式设值后, 后续启动都从 nps.conf 读(推荐: 升级后立即填一个固定值, 避免跨部署密文不兼容)
nps_master_key=
`
