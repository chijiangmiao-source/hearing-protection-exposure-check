# 耳罩佩戴后合成声级核算（Vue 3 + Go 1.25 net/http）

冲压车间更换耳罩后，检测员按六个倍频带录入现场声级 **L** 与厂家衰减值 **A**，系统逐行计算
**C = L − A**，再按对数能量叠加得到唯一的佩戴后合成声级：

```
E = 10 × log10( Σ 10^(C/10) )
```

> ⚠️ 不能对六个 C（或 L）取算术平均，否则结果错误。

内部计算全程保留 float64 未舍入值，仅在最终显示时按**十进制四舍五入保留一位小数**
（0.05 进位，round half away from zero，基于 `math/big.Rat` 精确实现，规避二进制尾数误差）。

## 架构

- `api/`：Go 1.25，**仅用标准库 `net/http`**，无数据库、无外部在线服务。
  - `POST /api/calculate`：整单计算端点。
  - `GET /health`：健康检查。
- `web/`：Vue 3 + Vite + TypeScript。
  - 生产构建为静态文件，由容器内 nginx 托管，并把 `/api` 同源代理到 Go 服务。
  - 前端**不做任何声级合成**：六个 C 值、公式代入式、未舍入值、显示值全部以 API 响应原样展示。
- `verify/`：Go 编写的**一次性验收服务**，对容器网络中真实运行的 api 与 web 做端到端断言。

## 目录

```
.
├── api/                 Go net/http API（计算 + 整单校验 + 测试）
├── web/                 Vue 前端（源码、Vitest、Playwright、Dockerfile、nginx.conf）
├── verify/              一次性验收服务（docker compose run verify）
├── docker-compose.yml
└── .env.example
```

## 用 Docker Compose 启动

需要 Docker（含 Compose v2）。

```bash
docker compose up --build -d
```

启动后：

| 服务 | 默认宿主机访问地址 | 覆盖变量 |
| --- | --- | --- |
| Vue 前端（nginx） | http://localhost:8080 | `WEB_PORT` |
| Go API | http://localhost:8081（容器内固定 8080） | `API_PORT` |

覆盖宿主机端口（两种方式等价）：

```bash
# 方式一：.env 文件
cp .env.example .env   # 编辑 WEB_PORT / API_PORT
docker compose up --build -d

# 方式二：命令行环境变量
WEB_PORT=9090 API_PORT=9091 docker compose up --build -d
```

停止与清理：

```bash
docker compose down
```

## 一次性验收服务 verify

`verify` 服务**不会常驻**：它等待 api、web 健康后执行一组端到端断言（健康检查、
合法整单的真实复算比对、缺行/额外频带/越界/两位小数/无穷值全部 422 且无部分结果泄漏、
经 nginx 代理与直连 API 结果一致），通过则退出码 0，否则退出码 1。

```bash
# 启动 api 与 web，随后运行一次性验收
docker compose up --build -d api web
docker compose run --rm verify

# 或者只跑验收（compose 会先把依赖的 api/web 拉起来）
docker compose run --build --rm verify
```

## 本地开发

### Go API（需要 Go 1.25）

```bash
cd api
go test ./...          # 公式、舍入、整单校验、HTTP 端到端
go run .               # 默认 :8080，可用 API_PORT 覆盖
```

### Vue 前端（需要 Node 20+）

```bash
cd web
npm ci
npm test               # Vitest：表单工具、API 客户端、组件联调（mock fetch）
npm run e2e:install    # 首次运行下载 Chromium
npm run dev            # Vite dev server，/api 自动代理到 http://localhost:8080
npm run e2e            # Playwright：真实 Go API + 真实页面的端到端联调
npm run build          # 类型检查 + 生产构建
```

Playwright E2E 会自动在本地拉起 `go run .`（:8080）与 Vite（:5173）。
若 Go 不在 PATH 上，可用 `GO_BIN=/path/to/go npm run e2e` 指定。

## API 契约

### `POST /api/calculate`

请求（必须恰好六行，频带按固定顺序）：

```json
{
  "bands": [
    { "frequency": 125,  "level": 90.0, "attenuation": 10.0 },
    { "frequency": 250,  "level": 92.0, "attenuation": 12.0 },
    { "frequency": 500,  "level": 95.5, "attenuation": 15.5 },
    { "frequency": 1000, "level": 100.0, "attenuation": 20.0 },
    { "frequency": 2000, "level": 98.0, "attenuation": 18.0 },
    { "frequency": 4000, "level": 94.0, "attenuation": 14.0 }
  ]
}
```

成功（200）：

```json
{
  "rows": [ { "frequency": 125, "level": 90, "attenuation": 10, "corrected": 80 }, "…" ],
  "formula": "E = 10 × log10( Σ 10^(C/10) )，其中 C = L − A",
  "substitution": "E = 10 × log10( 10^(80.0/10) + … + 10^(80.0/10) )",
  "exactLevel": 87.78151250383644,
  "displayLevel": "87.8"
}
```

上例六行 C 均为 80.0 dB：E = 80 + 10·log₁₀(6) ≈ 87.78 dB → 显示 **87.8 dB**。

### 校验规则（整单原则）

- 固定 125、250、500、1000、2000、4000 Hz 六行；**缺行、额外频带、频带顺序/取值错误**整单 `422`。
- 每行：`L` ∈ [40.0, 140.0] dB，`A` ∈ [0.0, 40.0] dB，均**最多一位小数**。
- 缺字段、类型错误、`NaN`、无穷值（如 `1e999`）、多余的顶层字段，整单 `422`。
- 422 响应携带字段级定位信息（`fields[].field` + `frequency`），且**不含任何部分计算结果**：

```json
{
  "error": "整单校验未通过，请修正标红字段后重新提交",
  "fields": [
    { "field": "level", "frequency": 250, "message": "250 Hz 的 现场声级 L 必须在 40.0 至 140.0 dB 之间，当前为 141" }
  ]
}
```

## 前端行为

- 表格固定六行，无增删入口。
- 提交始终整单发往 API；收到 422 后**清除旧结果**、标红并聚焦第一个出错字段，不展示任何部分 C 值。
- 任一输入被修改后旧结果立即失效（数据已不再对应当前表单）。
- 合法提交后展示：六个 C 值、公式与逐带代入依据、内部未舍入值、四舍五入一位的显示值——界面数字与 API 响应严格一致。

## 测试覆盖

| 层 | 位置 | 覆盖内容 |
| --- | --- | --- |
| Go testing | `api/*_test.go` | 能量合成公式、一位小数十进制舍入、缺行/额外频带/越界/小数位/NaN/无穷整单 422、HTTP 200/422 状态与无部分结果 |
| Vitest | `web/test/` | 输入解析与展示格式化、API 客户端 200/422、组件成功渲染与 422 清结果/聚焦 |
| Playwright | `web/e2e/` | 真实浏览器 + 真实 Go API + Vite：公式联调、422 整单拒绝、空表单定位、修改失效、固定六行 |
| verify 容器 | `verify/` | Compose 网络内对 api 与 nginx/web 的一次性端到端验收 |
