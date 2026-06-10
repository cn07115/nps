# nps Docker 镜像

> 轻量级、高性能的内网穿透代理服务器,支持 tcp、udp、http(s)、socks5、p2p、http 代理等协议。

## 快速开始

### 服务端 (nps)

```bash
docker run -d --name nps --restart=always --net=host \
  -v /your/conf/path:/conf \
  cn07115/nps:v0.26.35
```

Web 管理界面默认端口:`8080`
Web 默认账号:`admin` / `123`(首次启动后请立即修改)

### 客户端 (npc)

```bash
docker run -d --name npc --restart=always --net=host \
  cn07115/npc:v0.26.35 -server=your-server-ip:8024 -vkey=your-vkey -type=tcp
```

## 多架构支持

镜像支持以下架构(通过 buildx 多架构构建):
- `linux/amd64` (x86_64)
- `linux/arm64` (ARM 64-bit,如 Apple Silicon、AWS Graviton)
- `linux/arm/v7` (ARM 32-bit,如 Raspberry Pi)

## 配置

通过挂载 `/conf` 目录持久化 nps 配置:

```bash
-v /host/path/nps-conf:/conf
```

npc 客户端建议通过 web 管理端生成启动命令,无需配置文件。

## 更新

```bash
docker pull cn07115/nps:v0.26.35
docker pull cn07115/npc:v0.26.35
```

查看具体版本的更新内容:[GitHub Releases](https://github.com/cn07115/nps/releases)

## 镜像标签

- `latest` — 最新稳定版
- `v0.26.35` — 锁定具体版本(推荐生产环境使用)

## 链接

- 完整文档:[https://github.com/cn07115/nps](https://github.com/cn07115/nps)
- 更新日志:[RELEASE_NOTES.md](https://github.com/cn07115/nps/blob/master/RELEASE_NOTES.md)
- 反馈问题:[GitHub Issues](https://github.com/cn07115/nps/issues)

## 关于

本镜像由 [cn07115/nps](https://github.com/cn07115/nps) 维护,基于 [yisier/nps](https://github.com/yisier/nps) fork。
