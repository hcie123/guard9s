---
name: k8s-offline-diagnostics
description: Structured, evidence-first offline Kubernetes diagnosis from user-pasted kubectl output, Events and resource YAML.
---

# Kubernetes 离线诊断工作流

用于对用户提供的静态文本做调度、Node/Pod 风险、PDB/副本、容量、存储与 Event 分析。严禁连接集群或执行命令；遵循 Agent 文件中的安全规则与报告模板。

## 工作顺序

1. 清点输入类型、对象范围、采样时间、版本、缺失字段和截断。
2. 若用户给了 FailedScheduling Event，先按每条 message 逐项归因；不以通用经验覆盖直接证据。
3. 对照相关对象 YAML：Pod requests/limits、选择器、硬/软 affinity、tolerations、topology spread、绑定 PVC/PV、Node labels/taints/conditions/allocatable、PDB 和控制器副本数。
4. 做 request-based 容量近似并说明假设。总量够不代表单 Pod 可放置，也不代表真实 scheduler 会选择这些 Node。
5. 对任何不可见的约束或互相冲突的证据标 UNKNOWN；不因对象缺失而按“无约束”处理。
6. 输出引用证据的中文报告、最小只读补充命令、人工评审的修复方向和风险/回滚考虑。

## 证据标注

- **观察：**输入直接出现的字段、状态或 Event 原文。
- **推断：**由观察支持的解释，明确写“推断”。
- **未知：**输入未覆盖的条件，不得转换成 PASS。
- **矛盾：**例如旧 Event 与新状态冲突，列明时间并建议重新采样。

## guard9s 方法映射

| 主题 | 离线证据 | 必须说明的边界 |
| --- | --- | --- |
| Node / Pod 健康 | Conditions、phase、Ready、restart、lastState、Warning Events | `Running` 不等于 Ready 或业务健康；采样后可能变化 |
| PDB / 副本 | selector、healthy/desired、allowance、generation、owner chain 与 Ready 分布 | PDB 是瞬时状态；副本数量不证明 quorum |
| 调度 | nodeSelector、required affinity、taints/tolerations、pod affinity、topology spread、PVC/PV affinity | 无插件仿真；preferred、隐藏 profile、准入变化等不可见 |
| 容量 | allocatable、现有 Pod requests、待迁移 requests、pod slots | utilization != requests；装箱和并发变化无法完全仿真 |
| 存储 | PVC/PV phase、locality、node affinity、可见 CSI attachment | 无法从清单证明后端健康、数据一致性或真实 detach/attach 成功 |
| Event | type/reason/message/object/time/count | Event 可能过期/聚合，缺失不证明故障不存在 |

详细算法和限制说明见 [guard9s 方法说明](references/guard9s-method.md) 与 [输入清单](references/input-checklist.md)。
