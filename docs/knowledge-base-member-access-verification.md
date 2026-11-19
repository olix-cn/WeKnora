# 知识库成员权限验证记录

> 2026-11-17，开发工作树；未部署。管理路由与面板已挂载，下面历史记录中的“未注册”描述已被末节取代。受控测试范围见 [测试部署清单](knowledge-base-member-access-deployment.md)，不代表生产保密场景全部通过。

## PostgreSQL 实库

新增 opt-in 测试：`internal/application/repository/knowledgebase_members_postgres_test.go`。
仅指向**可销毁的独立测试数据库**，每例创建随机 schema，结束清理；默认不设置变量时跳过。

```bash
# DSN 由操作者提供，禁止指向现有测试服／生产业务库。
WEKNORA_KB_MEMBER_TEST_POSTGRES_DSN='<disposable PostgreSQL DSN>' \
  go test -race -p 2 -count=3 ./internal/application/repository -run '^TestKBMemberPostgres'
```

本次复用已有 PostgreSQL 17 镜像，临时容器只绑定回环地址、无业务卷／配置，使用 tmpfs 存储和合成账号；已停止并删除。

连续三轮通过：

- 实际执行 PG140 up/down SQL；restricted 或非空显式授权时 down 拒绝，清空后可回退。
- 多连接双保存同一版本恰好一个成功，另一方明确版本冲突。
- 保存受限策略与 KB 分享、Agent 分享、共享 Agent scope 扩展竞争，不能同时成功。
- 成员撤权与管理保存交错后重新检查当前有效身份，不能产生策略写入。
- 一个连接持有空间行锁，另一个连接保存触发 `55P03` lock timeout；释放后成功，证明锁不是单连接调度假象。
- 无用户、跨空间角色、synthetic 身份及带真实 owner ID 的 full-access API key 不能读取 restricted 库。

范围限制：已有模型表由测试 `AutoMigrate` 创建，新策略表来自真实 SQL；**不是从完整部署 schema139 迁移至140的演练**，也不是负载／多实例生产压测。

## 入口与任务回归

- `knowledge_transfer_member_test.go`（handler／service）：restricted 源库 copy/move 在入队前拒绝；旧排队任务执行前复查且无文档／分块／图谱写入。低层 clone/move 预检增加同一策略。
- `resource_storage_test.go`：IM 同空间文件捷径也必须检查 KB 策略，不因存储租户相同而通过。
- `member_scope_test.go`（MCP）：显式 endpoint KB ID 不绕过 restricted 规则。
- `session_tag_targets_test.go`／`knowledge_transfer_member_test.go`：KB 元数据缺失或查询失败时，不回退当前 tenant 构造检索／标签目标。旧测试要求“缺元数据继续检索”与新安全契约冲突，已替换为拒绝目标的断言，不放宽权限。
- 现有精确任务 grant 回归：已批准的单库后台写入可继续，但不得写其他库；API key 能力仍独立检查。

## 本轮发现并修复

1. IM outbound 在同空间分支提前通过，缺少 resource 成员守卫。已在捷径前加检查。
2. 问答 `buildSearchTargets` 对缺失 KB 默认采用 caller tenant。已改为没有证明则不构造 target。
3. 受限源库可读权限不足以证明可以复制到默认继承库。第一版先保守禁止 restricted 源库跨库内容转存，不自动复制／改变名单，也不猜两个受限库的受众等价。

## 后续补充验证：任务标记、历史回放与 Embed

- 新 `knowledge_base_transfer_fences`（仍在未发布的 PG140／SQLite56 脚本内）记录每次实际执行尝试。标记的创建和权限变更使用同一空间行锁；源／目标存在标记时管理保存返回 `policy_transfer_busy`。成功只删除本次标记；失败、panic或进程终止保留，不能使用TTL或成功重试自动清除早先的未知结果。暂无面向业务用户的恢复入口，需要明确核对后恢复，不能假装失败自动恢复。
- 转存 worker 的整个执行体在标记范围内；同步 CloneKnowledgeBase 也使用同一适配。PG隔离测试新增多连接执行期间源／目标策略拒绝，结束后可修改，更新脚本后连续3轮race通过。SQLite验证失败标记不会被同 task 成功尝试清除。down遇未恢复标记拒绝。
- 历史会话读取增加来源检查：全会话的 execution_context、knowledge_references、mentions、工具结构化字段指向的受限库若已不可读，则拒绝该会话回放及继续使用，不只删引用。聊天搜索过滤被拒会话；fork在源会话归属校验后重查。原始正文不改写，当前管理员／仍获授权成员保持可读。合成测试覆盖原文读取／分页／近期／按时间历史／继续入口在加载正文前拒绝。
- Embed身份认证＋KB中间件拒绝回归、Embed直接chunk正文读取拒绝回归已增加；后者原有 agent all/selected 判断不能代替KB权限。云URL包装层对 s3/minio/cos/oss/obs/tos/ks3/local 全部验证在调用native signer前拒绝受限资源。

## 收尾补充：恢复、列表和缓存

- failed恢复原语已实现：只允许当前真实空间owner/admin，精确任务坐标、停止worker／核对结果确认和说明；active及同task活跃兄弟尝试拒绝，无force／TTL。恢复审计和标记删除同事务，审计失败标记保留。service／handler仍未注册，不宣称已上线恢复入口。流程见 [转存异常恢复](knowledge-base-transfer-recovery.md)。PG真实脚本与恢复审计更新后又连续三轮race通过。
- 会话标题／数量与聊天产物／文档生成文件列表加入服务端当前权限排除列表，在count／分页之前应用；查询参数不能自带授权结果。列表扫描限制2000会话，超限报错而非不完整过滤，仍是待索引优化的过渡方案。
- Embed知识型建议问题测试验证受限KB不会触及chunk内容查询。历史follow-up缓存／再生成先重查会话权限，拒绝后不访问缓存或模型。ContinueStream新增回归：撤权在读取任何缓存事件及发送SSE头前返回403，不复用历史事件授权。

## 后续：来源索引与实时批次复查

- PG141／SQLite57新增来源版本、反向索引和覆盖前证据；由消息触发器失效，按变动版本刷新，证据原子压缩，来源单调保留。首次索引前覆盖／软删除的旧来源也保留。详见 [会话来源设计](session-knowledge-provenance.md)。
- 已索引大历史不再逐会话解码，仅对冷／变更会话回填；2100已索引会话反向过滤、显式游标回填和冷扫描超预算失败关闭均有回归。
- SSE实时和续流每批输出前复查；中途撤权后不输出下一批私有正文、不冲刷暂存文本、不发送假complete。已有连接输出字节和检查到发送之间的调度窗口不属于可追溯撤回。
- 证据表含历史引用／工具结果，是业务敏感数据，正式发布前须评审会话删除清理和保留期，不是“只含非敏感索引”。

## 仍阻止开放的事项及不可撤回边界

- 长任务并发窗口已用持久化标记保护常规worker和同步clone入口；failed有受限恢复原语与流程，active崩溃仅可维护核对后离线处理。仍需管理员标记列表／队列维护交互和低层调用审计，不能宣传一键恢复。
- 历史来源检查只覆盖已持久化且可解析的来源字段，不足以证明无来源老消息／模型改述／沙箱导出／长期记忆的完整数据血缘。扫描有行数／解析深度预算，需要进一步迁移为显式、可索引的会话来源关系；不宣称已经实现全站数据撤回。列表与缓存主要入口已接入，其他直接repo／导出和实时流中途撤权仍须审计。
- 已返回／已下载的数据不可收回；过去已签发的云厂商直链不经过本服务，通常持续有效到厂商签名到期。限制新签发不等于撤销旧URL；需要提供URL有效期／存储策略说明，若要求即时撤权须统一代理交付而非厂商直链。
- Embed建议问题、直接chunk、认证与流式恢复已有合成回归；跨空间运维、预览会话与真实集成端到端矩阵仍待验收。
- 管理服务／路由／面板仍未注册；Swagger、真实浏览器合成成员闭环、完整迁移／回退演练仍未完成。

不会自动部署或执行在线迁移。

## 最新验收补充：挂载、真实接口与运行基础

- 已挂载DI／路由／知识库详情对话框，详情与列表回显实际成员权限，避免旧同空间admin捷径误展示权限。机器principal显式拒绝借用人类账号。
- 首轮在检索／模型前记录来源，支持共享Agent的原caller tenant和机器owner；伪造目标tenant、别人的会话、软删会话拒绝。来源不因引用删除／fork丢失。
- PG141／SQLite57新增legacy_unverified：迁移前消息来源不完整时，本空间任一受限库不可读则保守隐藏旧会话及列表；管理员仍可读，不删除正文，不允许用户自行取消标记。
- 实际测试服纯schema快照无业务行，在独立PG17模板0数据库恢复后执行140／141成功；从历史SQL完整建立139再升级／空回退／来源回退拒绝也通过。第一次脚本把并发建索引放事务内、fixture漏business列已更正，未改业务迁移约束。
- 固定runtime内anydoc构建与ldd通过；隔离实际server+纯schema+合成账号真实JWT GET／PUT与reader拒绝通过。宿主机产物因glibc／GLIBCXX高于固定基础已判定不适用，不用来发布。
- 新router集成测生产路由＋service＋实际SQLite脚本，覆盖log-only RBAC仍拒绝、委派管理／候选、409与撤权。WebKit连接该真实后端保存与重开通过（其余页面依赖为fixture，无完整业务E2E声明）。
- 恢复状态GET最多100条，仅当前空间管理员；failed恢复沿用精确坐标、人工确认与原子审计，active无在线强清。
- 相关完整Go回归／race／vet及前端2762测试、类型检查、生产构建通过。长期记忆／导出治理、生产清除保留期、BrowserSkill新旧资源、云URL不可撤回仍是明确限制，不据此宣称全站撤回。
