# nps 项目规范(reins 公共约定)

各 rein 在执行任务前请先读 `../AGENTS.md` 的 Code style / Testing instructions / Domain notes;本文件只放 reins 共享的轻量规则,避免在每个 agent.md 里重复。

## 1. 提交纪律

- 改完不 commit、不 push;回报里只列 diff 文件 + 验证结果,由用户决定提交
- 一次提交只做一件事,中文 commit 标题 ≤ 30 字
- 涉及并发、加密、协议层的修复,body 写明根因 + 复现条件(参考 v0.26.34 那一长串修复条目)

## 2. 并发与资源(高频踩坑)

- 共享结构体字段读写必须加锁,参考 `bridge/bridge.go:32` 的 `sync.Mutex` + 注释约定
- `goroutine` 退出优先用 `context.Done()`,不要纯靠 channel 关闭
- 资源释放:`defer` 关闭 + 临时文件清理(参考 v0.26.34 "文件存储 panic 改为错误日志,defer 清理临时文件"修复)
- 缓冲区走 `sync.Pool`,错误路径也要归还(参考 v0.26.34 "muxPackager buffer pool 泄漏"修复)

## 3. 跨平台

- 文件后缀分流:`_windows.go` / `_nowindows.go`,禁止运行时 `if runtime.GOOS == ...`
- 网络系统调用: `sysGetsock_windows.go` / `sysGetsock_nowindows.go` 已分流,新增 net syscall 沿用
- 桥接传输: `server/proxy/transport.go` / `transport_windows.go` 同理

## 4. 依赖与版本

- Go toolchain: `go 1.24.0`,工具链自动选 `go1.24.9`
- `go.mod` 已 `replace github.com/astaxie/beego => github.com/exfly/beego v1.12.0-export-init`,**不要**改
- 新增第三方包: 走 `go get` + 写 `go.sum`,CI 跑 `make go-mod-tidy` 校验
- 版本号在 `lib/version/version.go` 和 `build.sh` 顶部的 `VERSION` 维护,发版时同步

## 5. 测试与验证

- 单测放同包 `*_test.go`,沿用 `-race -covermode=atomic`
- mux / pmux 改完必须三个测试都跑:`lib/nps_mux/mux_test.go`、`lib/pmux/pmux_test.go`、`server/proxy/tcp_test.go`
- 跨平台编译冒烟至少覆盖 `linux/amd64` + `windows/amd64` + `darwin/amd64`

## 6. 安全边界

- 客户端鉴权 vkey 走 `lib/crypt`,不要自造 hash
- TLS ClientHello 16KB 长度上限必须保留(`lib/crypt/clientHello.go`)
- p2p 走 STUN,生产建议强制 vkey + IP 白名单
- Web 管理端口对外暴露必须改默认账号密码

## 7. 与上游同步

- `origin` = `cn07115/nps`(本 fork),`upstream` = `yisier/nps`(上游)
- 同步上游:`git fetch upstream && git rebase upstream/master`,先 rebase 自己的 commit 再合
- 不要直接 push 到 `master` 之外的"长期分支",本 fork 就一个 master
- 不主动碰 remote 配置
