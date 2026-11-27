# 使用教程

管理员从“我的运维 → 教程管理”进入，用户从侧边栏“使用教程”阅读。
每篇教程可选择“仅登录可见”或“公开可见”，默认仅登录。写入接口仅限管理员。

## 公开分享

编辑器的“阅读权限”选择“公开可见”，再点击“发布”或“发布更新”。
复制阅读链接后，访客可免登录阅读文章和配图；列表、搜索、分类和上下篇只展示已公开的发布快照。
访客页面显示站点标识、教程导航和登录入口，登录后继续使用原站内阅读界面。
文章链接不随权限切换改变。

权限变更与内容一起发布，保存草稿不会改变已发布版本的权限。
切回“仅登录可见”并发布后，匿名正文及图片接口立即停止提供内容；已有文章默认仍仅登录可见。
未登录访问受限文章时不暴露标题和正文，提供登录后返回原链接的入口。
站点开启“仅后台模式”时继续遵守现有公开页面限制。

## 编写与发布

- 使用 Markdown 编写，桌面支持左右实时预览，手机切换编辑和预览。
- 支持教程模板、常用格式工具、撤销/重做、图片上传及粘贴截图。
- 自动保存和“保存草稿”只更新草稿。首次发布、发布更新分别生成用户可读的发布快照。
- 两个页面同时编辑会检测版本冲突，保留当前编辑文字并提示重新加载。
- 支持分类、置顶和排序；删除分类后文章移入“未分类”。
- 下架后用户无法获取文章和配图；草稿保留，可再次发布。
- 阅读页支持搜索标题、摘要和正文，并提供正文目录、代码复制、图片放大和上下篇导航。

## 教程包

在教程管理列表选择“导入教程包”可新建草稿。通过列表中对应教程的“覆盖导入”或在该教程的编辑器中导入，可替换当前草稿，保留教程编号与网址。确认预览后发布更新。旧发布版本的配图保留至发布更新，之后清理已无引用的文件。未指定阅读权限的教程包在覆盖时沿用目标草稿的阅读权限。

编辑器中的“自定义网址”可设置如 `/tutorials/codex-cli-setup` 的阅读地址。留空使用数字编号。自定义网址首次发布后固定，公开教程的旧数字链接会以 HTTP 301 跳转到该地址。ZIP 中不保存站点专用的教程网址。

在编辑器选择“导出教程包”可下载当前草稿及正文引用的配图。导出以图片资源编号作为文件名。

ZIP 根目录放一个 UTF-8 Markdown 文件（`.md` 或 `.markdown`），配图放在 `images/` 下。导出的 `tutorial.md` 顶部包含以下 YAML 信息，分隔线后是原 Markdown 正文。

```markdown
---
title: 快速开始
summary: 连接客户端的第一步。
category: 入门
visibility: login
pinned: false
sort_order: 10
---
## 配置客户端

![设置页面](./images/settings.png)
```

`visibility` 可填 `login` 或 `public`，默认 `login`。分类按名称匹配，目标站点没有该分类时会创建。没有 YAML 信息的文件使用首个一级标题或文件名作为标题。

教程包最大 52 MiB，Markdown 正文最大 1 MiB。配图支持 PNG、JPEG、GIF，单张最大 5 MiB、2400 万像素，每个包最多 200 张、合计 50 MiB。包内未被正文引用的图片通过校验后不会存入教程目录。

## 教程 API

在「我的运维 → 教程管理 → 教程 API 密钥」生成密钥，可设置到期时间或随时撤销。完整密钥只在生成时显示。通过本地环境变量 `TUTORIAL_API_KEY` 提供给调用工具，使用 `x-api-key` 请求头。`TUTORIAL_SITE_URL` 为站点地址，生产环境使用 HTTPS。

```bash
curl --fail-with-body --silent --show-error \
  --header "x-api-key: ${TUTORIAL_API_KEY}" \
  "${TUTORIAL_SITE_URL}/api/v1/admin/tutorials?page=1&page_size=20"
```

以下路径相对于 `/api/v1/admin/tutorials`。成功的 JSON 响应将结果放在 `data` 字段。

| 操作 | 方法和路径 | 请求参数 |
| --- | --- | --- |
| 列出教程 | `GET` 根路径 | `page`、`page_size`、`search`、`category_id`、`status`（`draft` 或 `published`） |
| 读取教程草稿 | `GET /{id}` | 使用数字 ID。返回草稿和当前 `version` |
| 新建草稿 | `POST` 根路径 | 教程字段 |
| 保存草稿 | `PUT /{id}` | 完整教程字段和当前 `version` |
| 发布、下架或删除 | `POST /{id}/publish`、`POST /{id}/unpublish`、`DELETE /{id}` | JSON `{"version": 1}`，替换为当前版本 |
| 读取分类 | `GET /categories` | 无 |
| 新建或编辑分类 | `POST /categories`、`PUT /categories/{category}` | JSON `{"name": "入门", "sort_order": 10}` |
| 删除分类 | `DELETE /categories/{category}` | 该分类下文章移到「未分类」 |
| 上传或读取配图 | `POST /{id}/assets`、`GET /{id}/assets/{asset}` | 上传时用 `multipart/form-data` 的 `file` 字段 |

教程字段包括 `title`、`summary`、`content`（Markdown）、`category_id`（可为 `null`）、`visibility`（`login` 或 `public`）、`pinned`、`sort_order` 和可选 `slug`。配图上传后，把返回的 `data.reference` 写成 Markdown 图片地址，如 `![配置截图](tutorial-asset:…)`。

更新已有教程前先读取草稿，在保留原字段的基础上提交改动。保存后用返回的新 `version` 执行发布。遇到 HTTP `409` 时重新获取草稿并核对差异，再决定如何合并。草稿、发布快照和阅读权限沿用教程管理页面的规则。

## 存储

迁移 `383_tutorial_center.sql` 创建独立的分类、文章和图片记录表。
文章保存草稿与发布快照，发布后的修改需要再次发布才对用户生效。
图片保存在应用 `DATA_DIR/tutorials/assets` 中，应与现有应用数据一起持久化和备份。
恢复教程需同时恢复数据库与图片目录。

配图仅支持 PNG、JPEG、GIF，单张不超过 5 MB，每篇最多 200 张、总计 50 MB。
服务端检查实际图片格式和像素数。Markdown 中使用站内图片引用，配图接口按发布版本的阅读权限检查。
外链图片和可执行嵌入内容不渲染。

## 本地验收

2026-11-17 在独立 WSL 应用、PostgreSQL 和 Redis 下验证。未操作远程服务器。
详细验证记录在工作区 `output/tutorial-feature-20261117/VERIFICATION.md`。
