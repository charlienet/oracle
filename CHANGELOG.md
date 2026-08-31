# 更新日志

本仓库的变更记录采用 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 风格，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

## [v1.1.0] - 2026-08-31

### 新增

- 批量写（INSERT ALL / 批量 MERGE / 逐行 RETURNING）经 `ensureWriteTx` 自管事务，保证多 chunk 分片写入要么全成要么全败
- 新增公开 API：`OutParam`/`InOutParam`/`InParam` 出参与 `RefCursor`（param.go）、`OracleGobSerializer`（serializer_gob.go）、`Locking` 行锁子句（clauses/locking.go）
- migrator 列迁移能力增强；驱动适配层（driver_adapter）完善，godror 预留以 build tag 隔离
- 单元与集成测试补充（59 文件 / 472 测试函数），新增 Oracle 11g/12c 集成测试编排脚本 `scripts/run-oracle-tests.sh`
- 新增 LIMITATIONS.md 限制说明与 docs/batch_insert_optimization.md 设计文档

### 变更

- go 指令从 1.25 降级至 1.22，全仓库在 Go 1.22 下可编译（`sync.WaitGroup.Go` 改写为传统的 `Add`/`Go`/`Done` 形式）
- 集成测试在未设置 `ORACLE_DSN` 时向 stderr 打印提示并正常退出，不再使用占位 DSN 强连
- `Config.DriverType` 补充完整 doc 注释，明确当前仅支持 go-ora；`Initialize` 对设置为非 "go-ora" 值的配置打印告警
- License 文件更名为 LICENSE，版权行补充 charlienet
- 新增 CHANGELOG.md（并从 .gitignore 的忽略列表中移除）

## [v1.0.13] - 2026-08-28

### 变更

- 枚举持久化修复与版本感知增强，全链路验证通过

## [v1.0.12] - 2026-08-24

### 变更

- 补充单元测试覆盖

## [v1.0.11] - 2026-08-21

### 修复

- 第 6 轮审查修复：零值回填、MERGE Vars 错位、重复查询、软删除检测

## [v1.0.10] - 2026-08-21

### 修复

- 第 5 轮审查修复：安全性、并发和测试覆盖

## [v1.0.9] - 2026-08-21

### 修复

- 修复 SQL 注入、命名冲突和代码质量问题

## [v1.0.8] - 2026-08-21

### 修复

- 修复多个代码质量和安全问题

## [v1.0.7] - 2026-08-21

### 修复

- 修复 MERGE 语句 UPDATE SET 子句中 excluded 别名不一致问题

## [v1.0.6] - 2026-08-11

### 修复

- 查询时补充大写列名键，修复显式小写 column tag 字段回填

## [v1.0.5] - 2026-08-11

### 变更

- DataTypeOf 将 json 类型统一映射为 CLOB

## [v1.0.4] - 2026-08-11

### 修复

- 修复完整代码审查发现的 20 项问题（MERGE 构建、migrator、保留字等）

## [v1.0.3] - 2026-08-11

### 修复

- 多行删除禁用 RETURNING，修复批量删除报错
- 修复批量创建 ID 回填错位

## [v1.0.2] - 2026-08-11

### 修复

- RETURNING INTO 字符串输出参数指定 Size，修复批量插入报错

## [v1.0.1] - 2026-08-10

### 变更

- 移除 go-funk 依赖，使用本地 utils 实现

## [v1.0.0] - 2026-08-08

### 变更

- Oracle 驱动完整化：回调体系、版本感知、驱动抽象层与测试套件
- 重写 README 匹配完整化后的驱动

## 开发过程

以下无 tag 归属、无法精确归入某个发布版本的提交，记录于此备查：

- fix: 修复集成测试发现的问题
- chore: go fix 现代化修复（`interface{}` -> `any`、`reflect.Ptr` -> `reflect.Pointer`）