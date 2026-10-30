# 企业统一开户与公司公共空间

## 目标与结论

管理员创建员工账户、分配公司空间角色；管理员在该空间配置一次模型、MCP、
智能体及检索参数，员工登录后直接使用。现有后端已支持这一资源模型，无需先改造
成全局共享资源。账户创建和空间分配目前是两个独立操作。

本文是实施方案。编写和核对方案不会修改运行中的注册策略、成员或资源归属。

## 当前测试环境

核对日期：2026-10-28。

- 唯一空间：`10000`，名称为 `李芾's Workspace`。
- `li.fu@scyhsj.com` 同时拥有系统管理员身份及该空间的 Owner 角色。
- 已有 4 个模型、2 条已保存智能体记录；MCP 服务尚未配置。
- 生效策略：`auth.registration_mode=self_serve`、
  `auth.default_tenant_mode=create_personal`、
  `tenant.self_service_creation_enabled=true`、
  `tenant.auto_accept_invitation=false`。

建议直接将空间 10000 作为公司公共空间，按实际公司名称更名。更名不改变空间 ID，
无需复制现有知识库、模型或智能体。不要为每位员工重复建立一个“公司空间”。

## 资源与权限边界

| 资源 | 共享方式与限制 |
| --- | --- |
| Chat / Embedding / Rerank / VLM 模型 | 按 tenant_id 配置，同空间复用，模型密钥由服务端使用 |
| MCP 地址、工具开关及空间级服务凭据 | 同空间配置一次；管理员维护 |
| MCP OAuth | 服务定义统一配置，但 token 按空间、用户主体及服务隔离，员工需各自授权 |
| 自定义智能体、内置智能体的空间配置 | 同空间成员可使用；Creator/角色决定谁可修改 |
| 知识库与文档 | 同空间成员默认可读取；Creator 所有权主要限制修改，不是私有阅读边界 |
| 检索参数、解析引擎、向量库、存储、搜索服务 | 空间级配置，管理员统一维护 |
| 正常 Web 会话 | 带用户作用域；历史/API 创建的无 user_id 会话存在兼容可见语义，不能宣称绝对私有 |
| 个人技能变量、个人 OAuth、个人浏览器连接 | 不应作为公司级凭据自动共享 |

同公司、同资料可见范围适合单一公共空间。部门间需要严格阅读隔离时，应拆分空间，
再通过组织共享选定的知识库/智能体。组织不会自动统一所有成员空间的模型和 MCP
配置；不要用放宽 tenant_id 查询条件的方式跨公司共享资源。

## 无需业务代码改造的落地方式

### 1. 设置管理员开户策略

在“设置 → 系统设置”保存以下运行时设置：

| 设置 | 建议值 | 作用 |
| --- | --- | --- |
| auth.registration_mode | invite_only | 禁止密码自助注册，管理员仍可创建账户 |
| auth.default_tenant_mode | tenantless | 新账户不自动获得个人空间及 Owner 角色 |
| tenant.self_service_creation_enabled | false | 普通员工不能自行创建其他空间 |
| tenant.auto_accept_invitation | true（使用邮箱邀请 UI 时） | 邀请已开户员工时立即加入，无需员工再次接受 |

最后一个开关不是直接添加成员 API 的前提。不需要全局自动接受时，可保持 false，
由 Owner 调用直加 API 完成管理员编排。

数据库运行时设置优先于环境变量。部署文件的回退配置可写为：

```dotenv
DISABLE_REGISTRATION=true
WEKNORA_AUTH_DEFAULT_TENANT_MODE=tenantless
WEKNORA_TENANT_SELF_SERVICE_CREATION_ENABLED=false
WEKNORA_TENANT_ENABLE_RBAC=true
```

`DISABLE_REGISTRATION=true` 映射到 invite_only，不是“允许新用户凭链接注册”。
若将来采用邀请注册，将运行时注册模式改为 invite_register。OIDC 新用户开通由 IdP
另行控制；关闭密码注册并不关闭 OIDC 首次开户。

### 2. 创建员工并加入公司空间

管理员 UI 流程：

1. 系统管理员在“系统设置 → 用户管理”创建员工，保存一次性随机密码。
2. 切到公司公共空间；由空间 Owner 在“成员”中按邮箱邀请并选择角色。
3. 若 auto_accept_invitation=true，已创建员工立即成为 active 成员；否则员工登录后接受邀请。
4. 员工首次登录。tenantless 用户已具有 active 成员关系时，登录逻辑会选取最早的
   有效成员空间，尽力保存为默认空间，并签发该空间作用域的 token。

API 编排等价于（使用管理员 JWT，不在脚本中硬编码密码或 token）：

```http
POST /api/v1/system/admin/users/create
Content-Type: application/json

{"username":"employee01","email":"employee01@example.com"}
```

password 省略时随机生成，仅本次创建结果返回。随后以公司空间 Owner 的激活上下文调用：

```http
POST /api/v1/tenants/10000/members
Content-Type: application/json

{"email":"employee01@example.com","role":"viewer"}
```

创建用户要求 SystemAdmin；直加成员要求目标空间 Owner（或单独授权的跨空间主体）。
不能仅因为操作者是 SystemAdmin 就跳过空间授权。

分配成员失败时账户仍已创建，应重试分配，不应再次生成或重置密码。重复身份的
创建请求可能返回已有账户，因此自动化必须核对返回的邮箱、用户名与 user ID；
已有 active 成员冲突也应读取实际成员角色后处理。

### 3. 分配最小必要角色

| 人员 | 推荐角色 | 能力 |
| --- | --- | --- |
| 公司空间负责人 | Owner | 成员管理、角色分配、API Key 与空间生命周期 |
| 模型/MCP/智能体维护人员 | Admin | 统一维护空间资源及基础设施 |
| 资料建设人员 | Contributor | 创建并管理自己的知识库/智能体；不能维护模型/MCP |
| 一般员工 | Viewer | 读取公司资料、使用智能体、创建自己的会话和提问 |

当前成员管理及发邀请路由是 Owner-only，空间 Admin 不具备这些写权限。
平台用户管理里的“普通用户/管理员”指系统管理员标志，不是空间四级角色。

## 若需要“一次提交就完成企业开户”

现有 CreateUserDialog 只提交 username/email/password，缺少目标空间与角色选择。
后续可以增加可选的“同时加入当前公司空间”，复用现有开户和成员服务：

1. 兼容保留旧请求；带目标空间时，服务端强制 tenantless 开户，校验目标空间存活和角色合法性。
2. 写入前校验操作者既有系统开户权限，又有目标空间成员管理权限。
3. 建立用户和成员关系，记录默认空间，审计账户创建和成员添加事件。
4. 保证重复提交不重置已有账户密码、不静默提升已有成员角色；处理邮箱/用户名冲突。
5. 保留一次性密码展示锁定机制；部分失败必须能恢复，不能隐藏已创建账户或丢失一次性密码。
6. 添加员工登录后可列出/调用公共模型、MCP、智能体，且不能修改它们的集成测试。

这是管理流程优化，不需要改写模型、MCP 或智能体的租户隔离规则。

## 验收

- 未登录调用普通注册及邀请注册在 invite_only 下均被拒绝。
- 新员工账户没有额外个人空间；分配 active 成员后首次登录落入公司公共空间。
- Viewer 看见公共知识库、模型和智能体，能问答；修改模型/MCP/他人智能体返回 403。
- 公司模型与 MCP 服务 ID 不因开户复制；OAuth 凭据不在不同员工间复用。
- 禁用员工账户后原 token 失效；移除成员后不再访问公司空间。
- 已有资料、模型绑定和检索参数不因切换注册策略而被复制或删除。

## 实现依据

- `internal/handler/system.go`：CreateSystemUser。
- `internal/types/user.go`：AdminCreateUserRequest。
- `internal/application/service/user.go`：AdminCreateUser、resolveLoginTenantID、resolveFirstMembershipTenant。
- `internal/handler/tenant_member.go`、`tenant_invitation.go`：直加成员与自动接受邀请。
- `internal/application/service/system_setting.go`：运行时设置及环境变量回退。
- `internal/application/repository/model.go`、`mcp_service.go`、`custom_agent.go`：空间作用域。
- `internal/application/repository/mcp_oauth.go`：OAuth 主体隔离。
- `internal/router/rbac.go`、`routes_auth_tenant.go`：角色与成员管理权限。
- `frontend/src/views/system/CreateUserDialog.vue`：现有管理员开户表单。
