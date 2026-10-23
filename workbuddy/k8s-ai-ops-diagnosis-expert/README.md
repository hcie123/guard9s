# K8s AI 运维诊断专家

WorkBuddy 专家 ZIP 的源目录。此版本采用 WorkBuddy 官方 Expert package 结构，提供基于用户粘贴内容的**离线、只读** Kubernetes 运维诊断提示流程。

## 能力与边界

- 分析用户粘贴的 `kubectl` 输出、Events 和 Kubernetes YAML。
- 按 guard9s 文档化方法组织 Node/Pod 风险、调度约束、PDB/副本、存储、资源 requests 与容量证据。
- 输出中文结论、原因、原始证据、推断、未知项、只读补充核查和人工评审的修复方向。
- 不连接 Kubernetes API，不读取 kubeconfig，不执行 kubectl 或 shell，不变更集群，不读取 Secret。
- 本 ZIP **不包含或运行 guard9s Go 二进制**；WorkBuddy 专家是由 `agents/*.md` 提示词定义的 LLM Agent，输出不等于 guard9s 程序诊断结果、实时集群检查或 kube-scheduler 仿真。

方法摘要基于 [guard9s](https://github.com/hcie123/guard9s) v0.4 源码与公开文档，遵循其 Apache-2.0 授权。项目版本、支持约束和方法局限以该仓库对应版本为准。

## 专家包结构

```text
k8s-ai-ops-diagnosis-expert/
├── .codebuddy-plugin/plugin.json
├── agents/k8s-ai-ops-diagnosis-expert.md
├── avatars/expert.png
├── skills/k8s-offline-diagnostics/SKILL.md
├── skills/k8s-offline-diagnostics/references/
└── README.md
```

结构和字段按 [WorkBuddy 官方专家开发文档](https://open.workbuddy.cn/docs/expert)；企业上传和权限配置按 [腾讯云 WorkBuddy 企业专家管理](https://cloud.tencent.com/document/product/1831/134421)。

## 构建与测试

在仓库根目录运行：

```sh
python3 scripts/package_workbuddy_expert.py
python3 -m unittest discover -s tests -p 'test_workbuddy_expert.py' -v
```

构建产物：`dist/k8s-ai-ops-diagnosis-expert.zip`。打包脚本校验官方要求的核心字段、Agent 文件名/frontmatter、相对路径、3 个 tags/quickPrompts、默认提示与首个 quickPrompt 一致、40–50 个中文描述汉字、ZIP 内目录层级和小于 20 MB 的限制。头像为 512×512 PNG 且低于 500 KB。

这些是本地结构和格式检查；WorkBuddy 客户端导入、Agent 对话质量和开放平台审核尚未验证。ZIP 不能据此描述成已发布或已通过平台解析。

## 上传与发布

如具有 WorkBuddy 企业管理员权限，按腾讯云官方企业专家管理页面：

1. 登录企业管理后台，进入 **AI 资源管理 → 专家管理 → 上传企业专家**。
2. 填写全局唯一专家标识、显示名称和分类，上传上述 ZIP。
3. 选择可见成员范围。建议先仅向小范围测试成员开放并保存草稿。
4. 在 WorkBuddy 客户端导入/调用专家，用脱敏合成 YAML、Pod/Node 输出和 Event 进行预览验收。
5. 验收后由管理员选择“保存并启用”；这一步才会对配置的成员发布。

个人/开放平台的专家市场创建和企业自建专家上传是不同流程。请按当前账号实际可见入口与官方最新页面操作；本仓库没有替用户上传、提交审核或发布。

## 来源与许可

- 上游项目：<https://github.com/hcie123/guard9s>
- 许可证：Apache-2.0；本 Expert 的新增提示词与参考说明按 Apache-2.0 一并提供。
- `NOTICE`/来源与局限详见仓库根目录与 `skills/k8s-offline-diagnostics/references/guard9s-method.md`。
