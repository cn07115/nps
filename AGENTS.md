# AGENTS.md

nps 是一款轻量级、高性能的内网穿透代理服务器,支持 tcp、udp、http(s)、socks5、p2p、http 代理等协议,自带 web 管理端,并提供 Windows/macOS/Linux 桌面 GUI 客户端。仓库模块路径 `ehang.io/nps`,当前版本 `0.26.34`,最低向下兼容 `0.26.0`。本仓库是上游 [yisier/nps](https://github.com/yisier/nps) 的本地 fork,本地 remote 名称 `origin` 指向 `cn07115/nps`,`upstream` 指向 `yisier/nps`,新功能开发直接在 master 上进行。

## Setup commands

- Go 工具链: `go 1.24.0`,toolchain 自动选取 `go1.24.9`(见 `go.mod`),本地需安装对应版本
- 安装依赖: `go mod download`
- 拉取 web 静态资源(可选,运行时自动释放): 首次 `git clone` 后无需额外动作,`web/embed.go` 已把 `web/static` 和 `web/views` 内嵌进二进制
- 构建: `make build`(等价于 `go build cmd/nps/nps.go && go build cmd/npc/npc.go`)
- 交叉编译多平台产物: `./build.sh`(需要 `gcc-mingw-w64-i686` + `gcc-multilib`,产 `*.tar.gz` 客户端包 + `npc_sdk.{dll,so,h}`)
- 桌面 GUI 开发: `cd cmd/npc/npc-gui/frontend && yarn install && yarn build`,再回到 `cmd/npc/npc-gui` 跑 `wails build`
- Android: `./build.android.sh`(通过 `fyneio/fyne-cross:android-latest` 容器)
- 跑测试: `make test`(等价于 `go test -failfast -race -coverpkg=./... -covermode=atomic ./...`)
- 覆盖率 HTML: `make cover`
- 格式化: `make fmt`(`gofmt -w -s` + `goimports -w` 全部 `.go` 文件)
- 静态检查: `make lint`(`golangci-lint --enable-all --disable=lll` + `misspell`)
- 清理依赖: `make go-mod-tidy`
- 一键 CI: `make ci`(build + test + lint + go-mod-tidy)

## Project layout

- `cmd/nps/` — 服务端入口二进制(`nps.go`),含管理脚本子命令、守护进程、Web 服务
- `cmd/npc/` — 客户端入口二进制(`npc.go`) + SDK 动态库(`sdk.go`) + `npc-gui/` Wails 桌面 GUI 子项目
- `server/` — 服务端核心: `server.go` 主控,`proxy/`(tcp/udp/http/https/socks5/p2p/websocket 代理实现,带 `tcp_test.go`),`connection/` 长连接管理,`tool/` 服务端工具
- `client/` — 客户端核心: 长连接、注册、HTTP 代理、健康检查等
- `bridge/` — 服务端 ↔ 客户端的桥接层,处理 mux、信号通道、文件通道、心跳/重连
- `web/` — Beego Web 管理端: `controllers/`、`routers/`、`views/`(HTML 模板)、`static/`(静态资源,通过 `embed.go` 嵌入二进制)
- `lib/` — 共享基础库: `common/`、`crypt/`(加密)、`nps_mux/`(多路复用)、`pmux/`(端口复用)、`conn/`、`file/`(JSON 持久化: `clients/hosts/tasks/global.json`)、`cache/`、`rate/`、`sheap/`、`daemon/`、`install/`(kardianos/service 包装)、`version/`、`config/`、`goroutine/`、`pool/`、`lru/`、`pprof.go` 内置 pprof
- `conf/` — 默认配置与示例 JSON 数据
- `docs/` — 文档站(Hugo 源文件 + md 文档)
- `gui/` — Android 端入口(`fyne` + `AndroidManifest.xml`)
- `image/` — README 截图与图标
- `build.sh` / `build.assets.sh` / `build.android.sh` — 多平台发布脚本
- `Dockerfile.nps` / `Dockerfile.npc` — Docker 镜像(`golang:1.24` builder → `scratch` 运行时)
- `.github/workflows/release.yml` — Release 工作流(交叉编译、GUI 打包、Docker 推送)
- `.travis.yml` — 历史 CI,实际已迁到 GitHub Actions

## Code style

- Go 标准风格,提交前跑 `make fmt`;CI 跑 `make lint`(默认 `golangci-lint --enable-all --disable=lll`)
- 显式互斥锁保护并发读写:`sync.Mutex` 配合细粒度注释解释保护哪个字段(v0.26.34 多次修复并发问题,遵循同一约定)
- 错误处理使用 `github.com/pkg/errors` 包装,日志统一用 `github.com/fatih/color` + `beego/logs`
- 平台差异通过文件后缀处理: `transport.go` / `transport_windows.go`、`sysGetsock_nowindows.go` / `sysGetsock_windows.go`
- 资源释放必须 `defer`,关键路径(文件、连接、临时文件、bridge mux)显式写明 goroutine 退出控制
- pprof 已内置(`lib/pprof.go`),诊断时直接访问
- 第三方包注意: `github.com/astaxie/beego` 已被 `go.mod` 中 `replace` 指向 `github.com/exfly/beego v1.12.0-export-init`,不要改这个 replace

## Testing instructions

- 单元测试: `make test`(底层 `go test -race -coverpkg=./... -covermode=atomic -coverprofile=coverage.txt ./...`),`lib/config/`、`lib/conn/`、`lib/nps_mux/`、`lib/pmux/`、`server/proxy/tcp_test.go` 有现成测试
- 新功能必须带测试: 同包内放 `*_test.go`,沿用 `-race` 模式,跨平台逻辑分别在 `_windows.go` / `_nowindows.go` 配对测试
- E2E / 集成: 暂无统一 e2e 框架,改协议或核心 mux 时手工验证两端: `nps -server`(server) + `npc -server=... -vkey=...`(client)
- 所有测试通过、`go vet` 无警告才能 push;CI 入口 `make ci`

## PR & commit conventions

- 当前默认分支: `master`(本 fork 直接在 master 开发,不要切到 `main`)
- Commit message: 沿用上游中文风格,前缀常见 `fix:` / `feat:` / `优化` / `添加` / `v0.26.xx` 版本号。**新建功能建议用 conventional commits(`feat:` / `fix:` / `refactor:` / `docs:` / `test:`),便于将来切到自动化 changelog**
- 一次提交只做一件事;涉及并发、加密、协议层的修复必须在 commit body 说明根因 + 复现条件
- 不直接 push 到 `master`(本 fork 是个人仓库,先 PR 自审;若想直接 push 也可以,但保留 `--force` 习惯)
- Release 由 `.github/workflows/release.yml` 在打 tag 后触发,本 fork 修改前先看一遍 CI 是否会因为私仓 secret 缺失而失败(`DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` / `GH_TOKEN`)
- 同步上游: `git fetch upstream && git merge upstream/master`,先 rebase 自己的 commit 再合并

## Security

- **不要 commit 任何 secret**:`.env`、私钥、API token、vkey 默认值不要进 git;`.gitignore` 已忽略
- 客户端鉴权 vkey 通过 `lib/crypt` 加密传输,新增协议层鉴权时复用 `crypt` 包,不要自造 hash
- TLS 握手已加 16KB ClientHello 长度上限(v0.26.34 修复,见 `lib/crypt/clientHello.go`),新引入的 TLS 处理路径务必沿用该限制
- p2p 默认走 STUN 穿透,生产建议强制 `vkey` + IP 白名单
- 服务端 Web 管理端口对外暴露时必须改默认账号密码(参见 `docs/`)
- 系统服务安装用 `kardianos/service`,避免在脚本里直接写 `systemctl` / `sc.exe`,否则 GUI/CLI/服务三种模式行为会不一致
- 跨平台构建脚本里出现的 `sudo` / `apt-get` 是发布机环境,本地开发不要照搬到 PR 流程

## Domain notes (nps-specific)

- 协议实现都在 `server/proxy/`,新增代理类型时: 注册到 `server/tool/`、补 `server/proxy/<name>.go`、在 web 管理 UI 暴露(`web/controllers/`)
- mux 改动影响所有连接路径,必须在 `lib/nps_mux/`、`lib/pmux/`、`bridge/` 三处一起评估
- 数据持久化走 `lib/file/`(JSON 文件),改 schema 时注意向前兼容(老 v0.26.0 客户端要能继续注册)
- 版本号在两处维护: `lib/version/version.go` 的 `VERSION` 常量、`build.sh` 顶部的 `export VERSION=`,发版时同步改
- GUI 子项目独立 go module(`cmd/npc/npc-gui/go.mod`),修改时分别测试 Wails 后端 + Vue 前端
