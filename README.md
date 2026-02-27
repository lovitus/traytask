# TrayTask

跨平台托盘任务管理工具（Windows / Linux / macOS）。

你可以在托盘图标右键打开管理台或新增任务，任务本质就是命令行指令（例如 `ping`、`curl`、脚本命令等）。

## 功能

- 任务类型
  - 长期执行（`long_running`）
  - 执行完即关闭（`one_shot`）
- 状态显示
  - 启用后在管理台显示绿色状态点
  - 支持查看运行中实时日志和最终结果
- Cron 重复执行
  - 每个任务可设置 Cron（当前使用 **6 段**格式，包含秒）
  - 长期任务可配置 Cron 触发时是否先结束旧任务
  - 一次性任务不需要额外处理旧任务
- 环境变量管理
  - 全局环境变量（统一注入全部任务）
  - 任务级环境变量（覆盖/补充全局变量）

## 快速开始

```bash
go mod tidy
go run .
```

默认会自动打开浏览器管理台，并启动托盘图标。

可选参数：

- `-open=false`：启动时不自动打开浏览器
- `-listen=127.0.0.1:38080`：固定管理台端口

## Cron 说明

本项目使用带秒的 Cron 表达式（6 段）：

```text
秒 分 时 日 月 周
```

示例：

- `*/30 * * * * *`：每 30 秒执行一次
- `0 */5 * * * *`：每 5 分钟执行一次（第 0 秒）
- `0 0 9 * * *`：每天 09:00:00 执行

## 数据存储

配置与日志默认存储在系统配置目录的 `traytask` 子目录：

- 配置：`config.json`
- 日志：`logs/<task-id>.log`

可通过环境变量覆盖数据目录：

```bash
TRAYTASK_DATA_DIR=/your/path go run .
```

## GitHub Release

已提供自动发布工作流：

- 文件：`.github/workflows/release.yml`
- 触发条件：推送 `v*` 标签（如 `v0.1.0`）
- 输出平台：Windows / Linux / macOS

发布命令示例：

```bash
git tag v0.1.0
git push origin v0.1.0
```

工作流会自动构建并把产物上传到对应 Release。
