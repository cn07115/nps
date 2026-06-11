# nps v0.26.37 更新日志

## 新增

- **ACME 全自动 SSL 证书管线**：启用 host 的"自动 SSL"并选 DNS 凭证后，nps 自动调用 Let's Encrypt 签证书并部署，无需手动粘 PEM/Key
- **证书状态可见**：新增 "SSL 证书状态" 页面，显示每张证书的申请中/已签/续期中/失败/过期状态、过期倒计时、绑定 host、最近错误，支持手动重签
- **host 列表 SSL 列**：域名解析列表新增 SSL 状态 badge，一眼看出哪些 host 已部署证书
- **host 编辑页查看自动证书**：启用 ACME 的 host 在编辑页可直接查看自动签发的 PEM/Key 全文

## 修复

- 修复 `web/views/ssl/index.html` 等模板引用不存在的 `header.html`/`footer.html` 导致 Docker 镜像启动 panic 的问题
- Docker 镜像改用 `FROM scratch` + 预解压 web 模板，镜像大小从 85MB 降至 10.9MB

## Docker

```bash
docker pull cn07115/nps:v0.26.37
docker pull cn07115/npc:v0.26.37
```
