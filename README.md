# 教室排课助手

教室排课助手是一个纯后端 RESTful API 服务，为学校和培训机构提供课程表编排、教室资源管理和冲突检测能力。

## 项目主要功能

- **基础数据管理**：教室、教师、班级、课程、时间段的完整 CRUD API。
- **智能排课算法**：根据学期周数、每周天数、每天节数和课程周课时要求生成课表，避开教师/班级/教室时间冲突，优先满足连排需求。
- **冲突检测与报告**：检测教师时间冲突、班级时间冲突、教室时间冲突、教室容量冲突和教师偏好冲突，并给出解决建议。
- **课表查询与导出**：按班级、教师、教室查询课表，支持 JSON / CSV 导出，支持按周次查看。
- **调课与手动调整**：支持交换两节课、移动单节课到空闲时段，自动重新检测冲突并记录调课历史。
- **课表草稿与发布闭环**：管理员可将当前课表另存为命名草稿、分页查看草稿列表与快照详情；发布前自动检查草稿冲突，无冲突时在单个数据库事务中替换当前课表，并记录发布人与发布时间。草稿名重复、草稿不存在、草稿存在冲突或已发布都会明确失败且原课表不变；同一草稿的并发发布只有一个请求成功，发布结果落库，服务重启后仍可回查。
- **统计与利用率分析**：教室利用率、教师工作量、课程分布热力图数据。

## API 文档

- Swagger UI：`/docs`
- OpenAPI JSON：`/swagger/doc.json`

## 快速启动

### Docker Compose（推荐）

```bash
docker compose --env-file .env up -d --build --wait
curl http://127.0.0.1:19515/healthz
```

停止并清理：

```bash
docker compose --env-file .env down -v --remove-orphans
```

### 本地运行

```bash
cd backend
go mod tidy
go run ./cmd/server
```

默认监听 `8080` 端口，SQLite 数据文件位于 `./data/gbschedule.db`。

## 主要 API 端点

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` / `/health` | 健康检查 |
| GET | `/docs` | Swagger UI |
| GET/POST | `/api/v1/classrooms` | 教室列表 / 新建教室 |
| GET/PUT/DELETE | `/api/v1/classrooms/:id` | 教室详情 / 更新 / 删除 |
| GET/POST | `/api/v1/teachers` | 教师列表 / 新建教师 |
| GET/PUT/DELETE | `/api/v1/teachers/:id` | 教师详情 / 更新 / 删除 |
| GET/POST | `/api/v1/classes` | 班级列表 / 新建班级 |
| GET/PUT/DELETE | `/api/v1/classes/:id` | 班级详情 / 更新 / 删除 |
| GET/POST | `/api/v1/courses` | 课程列表 / 新建课程 |
| GET/PUT/DELETE | `/api/v1/courses/:id` | 课程详情 / 更新 / 删除 |
| GET/POST | `/api/v1/time-slots` | 时间段列表 / 新建时间段 |
| GET/PUT/DELETE | `/api/v1/time-slots/:id` | 时间段详情 / 更新 / 删除 |
| POST | `/api/v1/schedules/generate` | 智能排课 |
| GET | `/api/v1/schedules` | 课表查询 |
| GET | `/api/v1/schedules/conflicts` | 冲突检测 |
| POST | `/api/v1/schedules/swap` | 交换两节课 |
| POST | `/api/v1/schedules/move` | 移动单节课 |
| GET | `/api/v1/schedules/adjustments` | 调课历史 |
| GET | `/api/v1/schedules/export` | 课表导出（JSON/CSV） |
| POST | `/api/v1/schedule-drafts` | 当前课表另存为命名草稿 |
| GET | `/api/v1/schedule-drafts` | 分页查看草稿列表（`page`、`page_size`） |
| GET | `/api/v1/schedule-drafts/:id` | 草稿详情（含完整快照） |
| GET | `/api/v1/schedule-drafts/:id/conflicts` | 检查草稿内冲突 |
| POST | `/api/v1/schedule-drafts/:id/publish` | 无冲突时事务内发布、替换当前课表 |
| GET | `/api/v1/schedule-publishes` | 发布记录回查（可按 `draft_id` 过滤） |
| GET | `/api/v1/statistics/classrooms` | 教室利用率 |
| GET | `/api/v1/statistics/teachers` | 教师工作量 |
| GET | `/api/v1/statistics/density` | 课程分布热力图 |

统一响应格式：

```json
{"code": 0, "message": "ok", "data": {}}
```

### 课表草稿发布闭环

1. 管理员把当前课表另存为命名草稿（草稿名全局唯一，重复返回 `409`）：

```bash
curl -X POST http://127.0.0.1:19515/api/v1/schedule-drafts \
  -H 'Content-Type: application/json' \
  -d '{"name":"期中考试前课表","created_by":"admin01"}'
```

2. 分页查看草稿列表与某份草稿的完整快照：

```bash
curl 'http://127.0.0.1:19515/api/v1/schedule-drafts?page=1&page_size=20'
curl http://127.0.0.1:19515/api/v1/schedule-drafts/1
curl http://127.0.0.1:19515/api/v1/schedule-drafts/1/conflicts
```

3. 发布时先检查草稿冲突：无冲突才在**单个数据库事务**内原子替换当前课表，并写入发布人、发布时间和发布记录；有冲突返回 `409` 且 `data.conflicts` 附带冲突明细，原课表不变：

```bash
curl -X POST http://127.0.0.1:19515/api/v1/schedule-drafts/1/publish \
  -H 'Content-Type: application/json' \
  -d '{"published_by":"principal"}'
```

失败语义：

| 场景 | HTTP / code | 原课表 |
| --- | --- | --- |
| 草稿名重复 | `409` / `40900` | 不变化 |
| 草稿不存在（详情/发布） | `404` / `40400` | 不变化 |
| 草稿存在冲突 | `409` / `40900`（返回冲突列表） | 不变化 |
| 草稿已发布（含重复点击/并发落败） | `409` / `40900` | 不变化 |

同一草稿的并发发布由数据库条件更新（`status = 'draft'` 才允许置为 `published`）和发布记录上的唯一索引共同保证只有一个请求成功；草稿状态、发布记录和替换后的课表均持久化到 SQLite，服务重启后可通过 `/api/v1/schedule-drafts` 与 `/api/v1/schedule-publishes` 回查。

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 语言 | Go 1.22 |
| Web 框架 | Gin |
| ORM | GORM |
| 数据库 | SQLite（github.com/glebarez/sqlite） |
| 参数校验 | go-playground/validator/v10 |
| 日志 | log/slog |
| API 文档 | swaggo/swag + swaggo/gin-swagger |

## 项目目录结构

```text
.
├── backend/
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/server/main.go
│   ├── docs/
│   └── internal/
│       ├── config/
│       ├── constants/
│       ├── dto/
│       ├── handler/
│       ├── middleware/
│       ├── model/
│       ├── repository/
│       ├── router/
│       └── service/
├── api/
├── deploy/
├── migrations/
├── docker-compose.yml
├── .env
├── .env.example
└── README.md
```

## 本地开发命令

```bash
cd backend
go mod tidy
go run ./cmd/server
```

## Docker 部署说明

- 后端服务内部端口固定为 `8080`。
- 宿主端口由 `.env` 中的 `BACKEND_PORT` 控制，默认 `19515`。
- SQLite 数据通过命名卷 `gbschedule_data` 持久化到 `/app/data`。
- 镜像使用 Go 多阶段构建，运行在 `alpine:3.20`。

## License

MIT
