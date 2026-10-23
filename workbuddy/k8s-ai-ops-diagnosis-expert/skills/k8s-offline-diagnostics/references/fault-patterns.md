# Kubernetes 常见故障模式与证据

本表指导对用户粘贴的静态资料做离线分析。状态名只是线索，不单独构成根因；每项命令都是建议用户在授权环境自行执行的只读检查，专家不会执行。

## Pod Pending / FailedScheduling

- **先看证据：**`kubectl describe pod -n <ns> <pod>` 中的 `FailedScheduling` Event、Pod `nodeName`、requests、nodeSelector、required affinity、tolerations、topology spread、PVC 引用；对应 Node 的 Ready、taints、allocatable 与已分配 requests。
- **常见原因：**`Insufficient cpu/memory/pods`、没有匹配 selector/affinity 的 Node、未容忍硬 taint、PV 拓扑冲突、PVC 未绑定、准入/调度门控。只能引用实际出现的 reason/message。
- **只读核查：**`kubectl describe pod ...`、`kubectl get nodes -o wide`、`kubectl describe node ...`、`kubectl get pvc -n <ns>`、`kubectl get events -n <ns> --sort-by=.lastTimestamp`。
- **建议/风险：**核对 requests、拓扑和存储约束，评估扩容或调整工作负载规格的审批与影响。总 CPU/内存充足不代表每个 Pod 都能装箱；不要给出调度成功保证。

## CrashLoopBackOff

- **先看证据：**container name、restartCount、state 与 lastState、terminated reason/exitCode/finishedAt、容器事件、探针定义、用户提供的脱敏日志。
- **常见原因：**进程退出、启动参数/配置错误、探针失败、依赖不可达、资源限制；不得仅凭 CrashLoopBackOff 指定某一原因。
- **只读核查：**`kubectl describe pod -n <ns> <pod>`；用户可自行运行 `kubectl logs -n <ns> <pod> -c <container> --previous --tail=100`，分享前删除凭据与业务数据。
- **建议/风险：**先定位退出码和最近变更，再由服务 owner 评审配置、镜像、探针或资源修正。不要建议重启或删除 Pod 作为诊断步骤。

## ImagePullBackOff / ErrImagePull

- **先看证据：**Event 原文、完整镜像引用的脱敏版本、imagePullPolicy、imagePullSecrets 名称引用、namespace、Node 条件，以及同镜像其他 Pod 的结果。
- **可能原因：**镜像名/tag 不存在、registry 鉴权/限流、证书或 DNS/网络不可达、架构不匹配。没有 registry 返回错误时保持 UNKNOWN。
- **只读核查：**`kubectl describe pod ...`、`kubectl get pod ... -o yaml`（不得包含 Secret 内容）、`kubectl get events -n <ns>`。
- **建议/风险：**核对镜像仓库、tag、拉取身份授权、节点到 registry 的网络路径。严禁请求或复述 dockerconfig、Token、Secret data。

## Node NotReady

- **先看证据：**Node Conditions（Ready/MemoryPressure/DiskPressure/PIDPressure/NetworkUnavailable）、Lease renewTime、taints、容量、Node 相关 Events、受影响 Pod 的范围和时间线。
- **可能原因：**kubelet 心跳中断、节点资源/磁盘压力、运行时故障、网络分区、证书或控制面通信异常；需结合实际 Condition message 和节点侧证据。
- **只读核查：**`kubectl describe node <node>`、`kubectl get nodes -o wide`、`kubectl get events -A --sort-by=.lastTimestamp`。用户可在已授权节点采集 `systemctl status kubelet`、`journalctl -u kubelet --since ...`、`df -h`、`free -h`，只分享脱敏片段。
- **建议/风险：**先判断故障域、业务副本/PDB 和节点恢复可能性，再由值班流程决定隔离或替换。只凭 NotReady 不建议驱逐或重启节点。

## OOMKilled

- **先看证据：**Pod/container `lastState.terminated.reason`、exitCode、restartCount、limits/requests、同一时间窗口的内存指标、Node MemoryPressure 与 Event。
- **判读边界：**`OOMKilled` 是内存回收/终止证据；单独看到 exit code 137 不能证明 OOM，SIGKILL 也可能由其他来源触发。`kubectl top` 只是采样时值。
- **只读核查：**`kubectl describe pod ...`、`kubectl top pod -n <ns> <pod> --containers`、`kubectl describe node <node>`；仅分析用户提供结果。
- **建议/风险：**检查内存增长、并发/缓存、limit 与工作负载行为；调高 limit 可能转移压力到 Node，需容量评估和 owner 审批。

## Cilium 网络故障

- **先看证据：**相关 Pod 的 namespace/name/Node、Cilium agent DaemonSet/Pod Ready 状态、Cilium status（若用户提供）、Cilium/Hubble 事件摘要、Service/EndpointSlice、NetworkPolicy、受影响流向和开始时间。
- **可能原因：**agent 未就绪、身份/策略拒绝、路由/隧道/MTU、EndpointSlice 或上游路径问题。没有 Cilium 自身诊断输出时不能断言是 Cilium 根因。
- **只读核查：**`kubectl get pods -n kube-system -l k8s-app=cilium -o wide`、`kubectl get events -n kube-system`、`kubectl get svc,endpointslices -n <ns>`、`cilium status`（后者由用户在已配置 CLI 的环境读取）。
- **建议/风险：**先从源/目标/协议/端口和策略证据缩小范围，再由网络/CNI owner 评审。不要建议清理 BPF map、重启 agent、改路由或放宽 NetworkPolicy。

## CPU、内存或磁盘资源不足

- **先看证据：**Pending Event 的 `Insufficient ...`、Node `allocatable`、已分配 Pod requests、目标 Pod requests/limits、Memory/Disk/PID Pressure、ephemeral-storage requests/limits、`kubectl top` 采样时间及节点 `df -h`（如有）。
- **判读边界：**scheduler 主要基于 requests 与约束，不以瞬时 CPU 使用率替代；磁盘容量还涉及 imagefs/nodefs、inode、ephemeral storage 和 kubelet eviction 条件。容量字段不完整时标 UNKNOWN。
- **只读核查：**`kubectl describe node ...`、`kubectl get pods -A -o wide`、`kubectl top nodes`、`kubectl top pods -A`、用户提供的 `df -h`/`df -i`。
- **建议/风险：**区别调度 request 不足与运行时利用率/压力，评估清理、扩容或规格变更的容量和业务影响。专家不提出删除文件或 Pod 的命令。

## PVC Pending

- **先看证据：**PVC phase、StorageClass、volumeBindingMode、accessModes、requested size、PVC Events、相关 PV、拓扑和 CSI provisioner Pod/Events（若提供）。
- **可能原因：**无默认/目标 StorageClass、provisioner 不可用、容量/拓扑不匹配、等待消费模式尚无调度 Pod、配额/策略问题。用 Event 验证，不按状态猜测。
- **只读核查：**`kubectl describe pvc -n <ns> <pvc>`、`kubectl get storageclass`、`kubectl get pv`、`kubectl get events -n <ns>`。
- **建议/风险：**核对存储类、区域/拓扑、provisioner 健康和申请规格。不要建议删除 PVC/PV；需先确认数据保留、回收策略和 owner 审批。

## 正常样本与证据不足

- 只有在采样完整且对应对象显示 Node Ready、Pod Ready、近期无相关 Warning Event 时，才可说“当前提供的快照未观察到该类故障”；不能泛称整个集群正常。
- 只给 `CrashLoopBackOff` 字样、单条无对象关联 Event、截断 YAML 或过期采样时，明确列出缺少的对象/时间/事件/状态信息，结论写 UNKNOWN，并只请求最小补充材料。
- 多来源冲突时保留两边证据与时间戳，建议重新采集；不要挑选支持某个猜测的一边。
