# 企业版和公众版打包版本

本规则用于企业版 `main`（`ent`）和公众版 `offcial-base`（`ob`），不改公众版分支拼写，也不改变更新接口或上游发布资产协议。

## 上游来源

`backend/scripts/resolve-version.sh` 优先读取当前提交的精确发布 tag，否则读取 `backend/cmd/server/VERSION`。企业版基线的版本文件及最近上游 tag 是 `0.2.17`；公众版基线两者都是 `0.2.15`，且不包含 `v0.2.17` 祖先；Owner 的 `v2.0.4` 仅为格式示例。`VERSION` 保持上游版本，不把打包后缀回写进去，以免下一次打包重复追加。`backend/cmd/server/EDITION` 分别记录 `ent` 和 `ob`。

## 打包事实与序号

统一入口为 `python3 backend/scripts/package-version.py --output <清单路径>`。输出示例为 `0.2.17-ent.20261018-1`，徽标和镜像 tag 添加一个 `v`。清单记录 `version`、`upstream_version`、`edition`、`build_date`、`sequence`、`timezone`、`built_at` 和完整 `source_sha`。

- 日期和时间都使用 UTC，按一次打包开始时间取值；不是提交日期，也不在浏览器中生成。
- 本地默认计数文件保存在 `${XDG_STATE_HOME:-$HOME/.local/state}/sub2api/package-versions/`。同一账号的两份检出共享计数，按版本分支和 UTC 日期分别从 1 递增；文件锁覆盖读取及写回。失败的打包也消耗序号，允许有间隙。需要跨机器协调时由发布方分配 `--sequence`，不得把新的空计数目录当已有发布序列继续使用。
- CI 的序号为 `github.run_id * 1000 + github.run_attempt`，不依赖有分页及并发竞争的镜像 tag 列表。`run_attempt` 必须小于 1000，重试生成新序号，允许间隙；清单通过 Actions artifact 保留。该序号不是“当日成功次数”，但在 CI 运行之间唯一且可回溯。
- `--version` 复用标识时必须同时传原 `--built-at`，并核对上游、分支、UTC 日期及序号。不同架构必须复用同一标识和时间，不能分别取号。

## 入口

- `make -C backend build`：分配一次标识，写入 `backend/bin/build-info.json`，注入 `main.Version`、`main.Commit`、`main.Date`；指定序号可用 `BUILD_SEQUENCE=<正整数>`。`LDFLAGS` 保留通用编译参数，不再用于另行覆盖版本身份。
- `deploy/build_image.sh`：外层分配一次标识后传给 Docker，镜像同时带不可变版本 tag 和本地 `latest`，`shan.source_sha` 标签记录完整来源。容器内 `/app/build-info.json` 与二进制使用同一清单。
- `.github/workflows/docker-image.yml`：仅允许 `main` 或 `offcial-base`，分别生成 `ent`、`ob`，两个架构消费相同的 `VERSION`、`COMMIT`、`DATE`、`UPSTREAM_VERSION`。分支移动 tag 继续保留。
- 直接调用 Docker 时必须传完整 `COMMIT` 以及外层已生成的 `VERSION`、`DATE`、`UPSTREAM_VERSION`；也可显式提供 `BUILD_SEQUENCE`。缺少身份时构建失败，不默认为序号 1。槽位脚本不传这些参数，因此 QA 必须消费开发提前构建并加上 `sub2api-slot:<来源SHA前12位>` tag、`shan.source_sha=<完整SHA>` 标签的镜像，不能依赖槽位临时裸构建。不得为此修改或绕过环境管理脚本。
- 上游原始 release/GoReleaser 的资产发布协议保留；它不是上述两条分支的企业/公众镜像打包入口。若后续要求企业版本通过该协议自动发布，需另经 Leader 确认范围，不能本单改写更新协议。

## 徽标状态

当前版本从公共配置的 `version` 读取，与后端 `/admin/system/version` 共用服务启动时注入的版本。自动更新检查关闭时依旧显示当前版本，只是不发自动检查请求；未检查、检查中、失败和已检查无更新分别显示对应提示。只有真实成功检查且无更新时显示“已是最新版本”。手动刷新继续传 `force=true`，不修改既有开关及竞态保护。现有后端比较函数会移除 `-` 以后的后缀，因此同一上游的不同打包标识不会被判为新上游版本。

## 自测与回退

运行 `python3 -m unittest discover -s backend/scripts -p test_package_version.py`、前端定向测试和全仓 `lint:check`；Go 的定向测试为 `TestPackagedVersionComparison`。产物清单、压缩包 SHA256 和构建来源 SHA 分开交付，不把构建输出提交 Git。部署回退仍由授权发布方按既有版本快照执行；本单不部署、不写原站。
