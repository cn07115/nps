# nps v0.26.35 更新日志

## 新增

- 客户端列表新增「到期时间」列，空值显示「无限制」
- 客户端新增/编辑页接入 flatpickr 日期时间选择器，支持 +7/+30/+90/+1年 预设与「清除」快捷，提交前严格校验格式
- 客户端列表 / 隧道列表 3s 实时刷新，切换后台标签自动暂停、离开页面停止，避免空闲时白打服务器

## 修复

- 新建客户端 ID 默认从 1 开始：NoStore 客户端改用 Id=-1 哨兵值，不再消耗真实 id
- 编辑客户端改回未到期时间后自动恢复 Status，npc 下次重连即可过校验，不再卡在 Validation key incorrect
- 首页 IP 限制 / P2P 端口 / 服务端 IP 配置空值兜底，默认配置也能正常渲染
- 版权年份 2018-2020 → 2018-2026
- `nps update` / `npc update` 自动更新地址切到本 fork (`cn07115/nps`)，避免拉到上游旧版
- Dockerfile 改用 `go mod download` + Go module proxy，修复 Docker build 在 `google.golang.org/protobuf` unshallow 时撞 GitHub 503 限流的问题

## 优化

- 补全缺失的 `web/static/css/flatpickr.min.css`，限制日历字体大小避免被基础样式放大

## Docker

```bash
docker pull cn07115/nps:v0.26.35
docker pull cn07115/npc:v0.26.35
```
