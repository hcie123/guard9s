# guard9s 诊断方法摘要

上游：<https://github.com/hcie123/guard9s>（Apache-2.0）。该专家借鉴并解释项目文档化方法；本文件不是 guard9s 计算引擎的运行结果。

## Findings 与结论

guard9s 的 finding 按 PASS、INFO、WARN、HIGH、CRITICAL 排序，每条含 category、namespace/resource、reason、evidence、recommendation；UNKNOWN 是证据不完整标记，不是另一种严重度。Readiness 汇总中 CRITICAL 为 BLOCKED，HIGH 或 UNKNOWN 为 NOT RECOMMENDED，只有 WARN 可形成 READY WITH WARNINGS。离线语言模型报告应保留“严重度”和“不确定性”两个维度，不得仅凭低严重度推导安全。

## 节点维护风险

将目标节点上的可迁移 Pod 作为需求，联合检查：

- PDB：匹配关系、allowance、健康计数、generation 是否已观察；预算应视为快照，且多 Pod 通常需串行等待预算恢复。
- Replica：controller owner 链、Ready 副本数、目标节点分布；未识别的自定义 CRD owner 要保留 UNKNOWN。
- Storage：Bound PVC/PV、PV node affinity、本地盘/hostPath、StorageClass、CSI node/attachment 可见性；清单无法证明后端健康或数据一致性。
- Node 与 Pod：Ready/Pressure/cordon、容器状态、重启、OOM 和相关 warning event。

该结论是维护前证据汇总，不是驱逐批准，也不保证应用可用。

## 调度约束

可核对 nodeSelector、required node affinity、NoSchedule/NoExecute taints 与 tolerations、required pod affinity/anti-affinity、硬 topology spread、PV affinity。preferred 约束属于软偏好。自定义调度器、插件 profile、隐藏 added affinity、准入 webhook 修改、feature gate、动态资源 claim、hostPort/hostNetwork、扩展资源、并发变化、未提供的 peer pod 等不能完整从文本确认，应标 UNKNOWN。

Event `FailedScheduling` 是直接证据但可能过时。逐条对照事件对象与时间，并使用当前只读快照核验。

## 容量近似

比较 Pod 资源 requests 与 Node allocatable 剩余请求量和 pod slots；需要时区分 init/sidecar、overhead 和 Pod-level request。不要将 `kubectl top` 利用率当作 scheduler 的 request 余量。先看总资源，再逐 Pod 检查 eligible Node；即使近似装箱成功，碎片、存储附件限制、scheduler 策略和新增竞争都可能改变结果。
