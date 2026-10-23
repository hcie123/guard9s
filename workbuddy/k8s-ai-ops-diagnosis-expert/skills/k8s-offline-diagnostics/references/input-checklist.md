# 离线输入清单

请让用户粘贴最小范围的脱敏只读输出。命令只是用户可选的本地采集建议；WorkBuddy 专家不会代为执行。

## Pending / FailedScheduling

- `kubectl describe pod -n <namespace> <pod>`：Events 与调度状态
- `kubectl get pod -n <namespace> <pod> -o yaml`：requests、nodeSelector、affinity、tolerations、topology spread、PVC 引用
- `kubectl get nodes -o wide` 与目标节点的 `kubectl describe node <node>`：conditions、taints、allocatable、已分配 requests
- `kubectl get events -n <namespace> --sort-by=.lastTimestamp`

## 维护风险

- 目标 Node 上 Pod 清单、各工作负载 owner 与当前 Ready/available replicas
- `kubectl get pdb -A -o yaml`（可先筛选相关 namespace）
- 相关 PVC/PV/StorageClass/CSINode/VolumeAttachment 的状态和 node affinity，注意移除 volumeHandle、云资源 ID 等敏感标识
- 采样时间、Kubernetes 版本、受影响业务/维护目标；无需提供生产账号或访问凭据

## 风险提示

- 删除所有 `kind: Secret` 文档及 kubeconfig。删不干净的 token、证书、用户标识、内网 DNS/IP、云账号/资源 ID 应先替换成一致的合成别名。
- 不要贴完整的 `kubectl get all -A -o yaml`，它既庞大又容易含敏感信息，也不能覆盖所有资源类型。
- `kubectl top` 只代表采样时利用率，不包含 scheduler 的全部决策约束。
- Event 的次数可能是聚合计数；记录其 first/last time 和对象 UID（若 UID 已脱敏则保持稳定别名）。
