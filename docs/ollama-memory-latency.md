# Ollama 记忆召回冷启动与 SSE 标题收尾

## 适用范围

本补丁针对 Ollama embedding 的可选驻留参数及 Agent SSE 标题跨轮询状态。
没有放宽记忆查询的 2 秒超时，没有改变默认模型配置、自动加载模型或迁移数据库。
本地源码修改不会自动更新已运行的发布镜像。

## Ollama：预热与驻留是两个不同环节

冷启动时模型加载可能超过 2 秒；`keep_alive` 只控制请求之后的驻留时间，
不能让第一次冷加载变快，也不能保证 Ollama 重启、资源压力或模型切换后仍驻留。

经管理员确认后，可在本机对已安装的模型执行合成文本预热：

```bash
curl --fail --silent --show-error --max-time 30 \
  http://127.0.0.1:11434/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":"Synthetic warm-up","keep_alive":"30m"}' \
  -o /dev/null
```

- 应在 Ollama 启动完成后或业务开始前执行，而不是在用户问答的 2 秒预算内执行。
- 预热会占用模型内存；应核对 `/api/ps` 的 `expires_at`、实际内存与业务负载。
- 长时间空闲后的冷请求仍可能超时；按业务需求安排预热，或采用服务级驻留策略。
- 不应为演练主动卸载生产模型，也不应循环无限重试或无限驻留。

补丁增加本地 embedding 模型现有 `parameters.extra_config` 中的可选参数：

```json
{"keep_alive":"30m"}
```

只展示要合并的字段，不要用它覆盖整个模型配置。现有客户端是否提供编辑入口需另行确认。
只有部署补丁后的本地 Ollama embedding 适配器会读取该参数；未设置时不传此字段，保持服务端默认行为。
支持非负 Go duration（例如 `30m`、`1h`）、`0`（请求后卸载）和 `-1`（无限驻留，不推荐）。
非法值在创建 embedder 时返回错误，远程 embedding 路径不变。

可选择 Ollama 服务级默认驻留配置替代模型级设置，但该操作涉及部署配置及可能的重启，
本补丁不代为实施。其他客户端或服务端资源调度仍可能影响驻留，不能仅凭参数设置就宣称冷启动已消除。

### 验证

默认单测只访问 mock HTTP 服务，不调用生产模型。需要合成文本 live 验证时显式设置：

```bash
WEKNORA_OLLAMA_TEST_URL=http://127.0.0.1:11434 \
WEKNORA_OLLAMA_TEST_MODEL=bge-m3:latest \
go test ./internal/models/embedding -run '^TestLiveOllamaEmbeddingWarmBudget$' -count=1 -v
```

该测试确认模型已安装，使用较长预算预热，再经过真实 Go embedder 路径做 5 次各 2 秒预算的调用；
不写入知识库或记忆数据。测试会使模型临时驻留 30 分钟，不证明任意输入和并发负载都满足预算。

## SSE：记住已发出的标题

`internal/handler/session/stream.go` 的 `titleReceived` 应属于整个连接，而非某次轮询。
标题早于完成事件到达时，保留其接收状态，避免回答完成后再次等待 3 秒。
标题确实晚到或缺失时，保留原有有界等待；用户停止、断连与取消后的完成事件处理不变。

回归测试覆盖标题提前、同批、晚到、缺失、空轮询、停止和断连场景。

## 部署与回退

- 使用包含修复的源码构建 app 镜像，并在测试环境验证 SSE 收尾和语义召回。若只更新 app，给它配置独立镜像标签，避免改动影响多个服务的全局版本变量。
- 按目标版本的升级指南核对数据库兼容性。这两项修复本身不新增迁移，但这不代表任意基线之间的升级都不需要迁移。
- 按需设置目标模型的 `extra_config.keep_alive`，不要覆盖其他模型参数。若使用预热，建议安排在 Ollama 启动后或业务开始前。
- 回退前确认数据库与目标镜像兼容。保留上一版镜像与原参数值；回退时仅恢复这次变更的镜像和模型字段。
