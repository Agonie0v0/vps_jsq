# VPS 剩余价值计算器

## 🚀项目简介

VPS 剩余价值计算器是一个帮助用户精确计算 VPS 产品剩余价值和剩余时间的工具。

只需要输入续费金额，选择对应的付款周期、到期时间和交易日期，即可获取 VPS 剩余价值、剩余天数等信息。

计算器支持多种支付货币和付款周期，外币汇率每日自动更新，支持自定义汇率计算，并提供一键分享计算结果图片功能。

## ⚡功能特点

- 交互合理
- 简洁清晰的计算结果
- 汇率每日自动更新，并且支持自定义汇率计算
- 更准确的算法，比如大小月非直接取30天计算
- 一键导出计算结果为图片，支持分享
- 适应不同的屏幕，电脑和手机浏览器上体验良好
- 首创 SVG 图片分享，速度更快、体验更佳、流量更省


## 💻在线示例

https://tools.196000.xyz/jsq

## 📷运行截图

![VPS 剩余价值计算器](docs/screenshots/jsq.png)

## 🐳 Docker 部署

需要 Docker Engine 和 Docker Compose。克隆仓库后，在项目目录运行：

```shell
docker compose up -d --build
```

访问 `http://localhost:8088`。如需更换主机端口，可在启动时设置 `HOST_PORT`：

```shell
HOST_PORT=9000 docker compose up -d --build
```

前端页面和 Go API 由同一个容器提供，不需要单独构建或配置前后端地址。查看日志和停止服务：

```shell
docker compose logs -f
docker compose down
```

`dist/` 是历史静态发布产物，不参与当前 Docker 镜像构建。

汇率来源可通过环境变量选择，默认使用 Frankfurter 的混合参考汇率。需要调整来源或端口时，先复制配置模板：

```shell
cp .env.example .env
```

然后编辑 `.env`，例如：

```dotenv
RATE_PROVIDER=frankfurter
SITE_NAME=ByteVibe VPS 计算器
SITE_DESCRIPTION=计算 VPS 剩余价值与剩余时间
SITE_KEYWORDS=VPS,剩余价值,计算器
SITE_URL=https://calc.example.com
SITE_ICON_URL=https://calc.example.com/icon.svg
```

`SITE_NAME` 用于页面标题、主标题和 SVG 分享图的“由 … 提供”；`SITE_DESCRIPTION` 和 `SITE_KEYWORDS` 设置 SEO 元信息；`SITE_URL` 是不带路径的公网站点 origin，用于 canonical 地址和分享 SVG 链接；`SITE_ICON_URL` 可设为完整 HTTP(S) URL 或站点内绝对路径。未设置时使用项目默认名称与描述，域名按当前请求推断，不注入自定义图标。

也可以切换到 ExchangeRate-API：

```dotenv
RATE_PROVIDER=exchangerate-api
EXCHANGERATE_API_KEY=你的_API_Key
```

未配置 Key 时会使用 ExchangeRate-API Open Access 接口。两种来源都由 Go 服务端请求和缓存 24 小时；`RATES_URL` 可覆盖上游地址，但响应格式仍须匹配所选 provider。Frankfurter 混合汇率的使用说明见其[数据许可](https://frankfurter.dev/license/)；直接指定单个数据提供方时，还需遵守该来源自己的条款。

正式接口为 `POST /api/v1/calculate` 和 `GET /api/v1/rates`。分享图地址为 `/share/<id>.svg`。旧的 `/api/vps/jsq`、`/api/vps/rates` 和 `/uploads/images/jsq/<id>.svg` 路径仍保留兼容。价值按付款周期自然日长度折算：周期起点由到期日向前推对应月数，月末日期会按目标月份最后一天处理；年费按付款周期折算成年价并保留输入币种。SVG 使用官方返回图的画布尺寸、渐变、表格布局、字段和页脚，分享链接自包含结果，可跨重启和多副本访问。金额计算在服务端完成。

如需不经 Docker 直接本地运行，需安装 Go 1.23 或更新版本，然后执行 `go run .`，默认访问 `http://localhost:8080`。

## 📢其它讨论

- [剩余价值计算器源由](https://www.nodeseek.com/post-172415-1)
- [计算结果 SVG 图片分享，速度更快、体验更佳、流量更省](https://www.nodeseek.com/post-291879-1)
- [cloudflare pages 免域名免空间 0 成本部署 vps 剩余价值计算器](https://www.nodeseek.com/post-319981-1)

## 📧我要反馈

[hello@196000.xyz](mailto:hello@196000.xyz)
