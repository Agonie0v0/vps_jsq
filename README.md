# VPS 剩余价值计算器

用于估算 VPS 在转移或出售时的剩余价值。输入续费价格、付款周期、到期日期和交易日期，即可查看剩余天数与折算价值；填写人民币成交金额后，还会显示相对估值的溢价或折价。

## 项目来源

本项目基于 [hahabye/vps_jsq](https://github.com/hahabye/vps_jsq) 二次开发。感谢原作者 hahabye 开源 VPS 剩余价值计算器及其早期实现。本仓库在原项目基础上继续维护，并增加 Go 服务端、Docker 部署、资产账单式页面、交易金额与溢价值计算、汇率服务端缓存，以及可直接分享的 SVG 资产快照。

项目遵循 [GPL-3.0](LICENSE.txt)。基于本项目修改或分发代码时，请遵守许可证要求，并保留原项目及本项目的版权和许可声明。

在线体验：[VPS 剩余价值计算器](https://jsq.bytevibe.dev)

## 功能

- 按到期日、交易日和付款周期计算 VPS 剩余天数与剩余价值，按实际日历月长度折算。
- 支持多种续费货币和付款周期；外币按参考汇率折算成人民币，也可输入自定义汇率查看自定义估值。
- 可选填人民币成交金额，展示成交价相对剩余价值的溢价或折价；有不同的自定义汇率时，以自定义估值为比较基准。
- 生成包含站点图标、估值方式、续费方案和交易对比的 SVG 分享图。未配置站点图标时使用内置品牌图标。
- 桌面和移动浏览器均可使用。

## 分享图示例

以下示例包含自定义汇率、成交金额和折价结果：

![VPS 剩余价值 使用示例](https://img.bytevibe.dev/oneimg/2026/10/18e2ba8fb3b6455a240_305ccd6f-ed6e-474c-b239-121bd5704b57.webp)
![VPS 剩余价值 SVG 分享图示例](docs/screenshots/jsq.png)

## 部署

需要 Docker Engine 和 Docker Compose。部署已发布的镜像时，只需要准备一个目录，下载环境变量模板并复制为 `.env`：

```sh
curl -fsSL https://git.bytevibe.dev/gary/vps_jsq/raw/branch/main/.env.example -o .env.example
cp .env.example .env
```

在同一目录创建 `docker-compose.yaml`：

```yaml
services:
  jsq:
    image: git.bytevibe.dev/gary/vps_jsq:latest
    container_name: jsq
    restart: unless-stopped
    env_file:
      - .env
    ports:
      - "${HOST_BIND_ADDRESS:-127.0.0.1}:${HOST_PORT:-8088}:8080"
    volumes:
      - jsq-data:/data

volumes:
  jsq-data:
```

启动和停止服务：

```sh
docker compose up -d
docker compose logs -f
docker compose down
```

默认服务只绑定 `127.0.0.1:8088`。如需更换端口或绑定地址，修改 `.env` 中的 `HOST_PORT` 和 `HOST_BIND_ADDRESS`。

### Traefik

如果使用 Traefik，可以直接使用仓库中的覆盖文件：

```sh
curl -fsSL https://git.bytevibe.dev/gary/vps_jsq/raw/branch/main/docker-compose.traefik.yml -o docker-compose.traefik.yml
```

将 `.env` 中的 `TRAEFIK_ENABLED` 设为 `1`，确认 Traefik 已连接到 `${TRAEFIK_NETWORK}` 外部网络，然后使用基础文件和覆盖文件启动：

```sh
docker compose -f docker-compose.yaml -f docker-compose.traefik.yml up -d
docker compose -f docker-compose.yaml -f docker-compose.traefik.yml logs -f
docker compose -f docker-compose.yaml -f docker-compose.traefik.yml down
```

覆盖文件会移除主机端口映射，并加入 Traefik labels 和外部网络。`TRAEFIK_HOST`、`TRAEFIK_ENTRYPOINTS`、`TRAEFIK_ROUTER_NAME`、TLS、证书解析器和中间件都从 `.env` 读取。`TRAEFIK_MIDDLEWARES` 指向的中间件必须先在 Traefik file provider 中定义；不使用中间件时，请从覆盖文件删除对应 label。

页面和 API 由同一个 Go 服务提供，不需要单独构建前端。`dist/` 是历史静态发布产物，不参与当前 Docker 镜像构建。

主页页脚展示今日和累计成功计算次数，数据保存在 SQLite。Docker Compose 默认将数据库保存在 `jsq-data` 持久化卷中；可用 `DB_PATH` 修改容器内路径。计算接口默认按客户端 IP 每分钟限制 30 次，可通过 `CALCULATE_RATE_LIMIT` 和 `CALCULATE_RATE_WINDOW` 调整；达到限制时返回 HTTP 429。

### Gitea Actions 发布

`.gitea/workflows/release.yml` 会在推送任意 tag 时运行测试，构建并推送 `linux/amd64` 和 `linux/arm64` 两种架构的镜像，同时创建对应的 Gitea Release，并更新 `latest` 标签。

请在仓库的 Gitea Actions secrets 中配置以下变量。Secret 名称特意不以 `gitea` 开头：

| Secret | 说明 |
| --- | --- |
| `REGISTRY_HOST` | Gitea 容器镜像仓库地址，例如 `git.bytevibe.dev` |
| `REGISTRY_USERNAME` | 镜像仓库登录用户名 |
| `REGISTRY_PASSWORD` | 镜像仓库密码或访问令牌 |

创建 Release 使用 Gitea Actions 自动提供的 `${{ secrets.GITEA_TOKEN }}`，无需额外创建令牌 Secret。Workflow 为该 job 请求 `releases: write` 权限；如果仓库或实例的 Actions 权限上限未允许写 Release，需要在对应设置中放开。

推送版本示例：

```sh
git tag v1.0.0
git push origin v1.0.0
```

## 配置

复制环境变量模板并按需修改：

```sh
cp .env.example .env
```

主要配置项：

| 变量 | 说明 |
| --- | --- |
| `HOST_BIND_ADDRESS`、`HOST_PORT` | 直连模式的主机绑定地址和端口，默认 `127.0.0.1:8088`；Traefik 模式不发布主机端口 |
| `TRAEFIK_ENABLED` | Traefik 覆盖文件开关：启用 `docker-compose.traefik.yml` 时设为 `1`，直连为 `0` |
| `TRAEFIK_HOST` | Traefik 路由匹配的域名 |
| `TRAEFIK_NETWORK` | Traefik 已连接的 Docker 外部网络名称，默认 `frontend` |
| `TRAEFIK_ENTRYPOINTS` | Traefik entrypoints，默认 `https` |
| `TRAEFIK_ROUTER_NAME` | Router 和关联 service 名称，默认 `vps_cal` |
| `TRAEFIK_TLS`、`TRAEFIK_CERTRESOLVER` | 是否启用 TLS 及证书解析器名称 |
| `TRAEFIK_PRIORITY` | Router 优先级，默认 `10` |
| `TRAEFIK_MIDDLEWARES` | 可选中间件列表，默认 `real-ip@file`；需在 Traefik file provider 定义 |
| `RATE_PROVIDER` | 汇率来源：`frankfurter` 或 `exchangerate-api` |
| `EXCHANGERATE_API_KEY` | ExchangeRate-API 密钥，可留空使用其 Open Access 接口 |
| `RATES_URL` | 可选的兼容汇率接口地址覆盖项 |
| `SITE_NAME` | 页面和分享图中的站点名称 |
| `SITE_DESCRIPTION`、`SITE_KEYWORDS` | SEO 描述和关键词 |
| `SITE_URL` | 公网站点 origin，不带路径；用于 canonical 地址和分享链接 |
| `SITE_ICON_URL` | HTTP(S) 图标地址或站点内绝对路径；留空时使用内置品牌图标 |
| `DB_PATH` | SQLite 数据库路径，默认 `/data/vps-jsq.db` |
| `CALCULATE_RATE_LIMIT`、`CALCULATE_RATE_WINDOW` | 单个客户端在限流窗口内可调用计算接口的次数和窗口长度，默认 `30` 次/`1m` |

默认汇率来源为 Frankfurter。服务端会请求并缓存汇率 24 小时；如选择其他数据来源，请遵守其使用条款。Frankfurter 的数据许可见[官方说明](https://frankfurter.dev/license/)。

## API 与分享链接

- `POST /api/v1/calculate`：计算剩余价值并返回分享链接。
- `GET /api/v1/rates`：获取当前汇率。
- `GET /share/<id>.svg`：访问计算结果 SVG 分享图。

旧路径 `/api/vps/jsq`、`/api/vps/rates` 和 `/uploads/images/jsq/<id>.svg` 仍保留兼容。计算金额由 Go 服务端完成；分享链接包含计算所需的结果参数，可独立呈现快照。

## 本地运行

需要 Go 1.26 或更新版本：

```sh
go run .
```

默认访问 `http://localhost:8080`。运行测试：

```sh
go test ./...
```


## 问题与建议

遇到问题或有功能建议，欢迎在 [ByteVibe Gitea Issues](https://git.bytevibe.dev/gary/vps_jsq/issues) 提交 Issue。
