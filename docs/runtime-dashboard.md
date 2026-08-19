# 运行审计后台

运行后台与 AinP 使用同一个 HTTP 服务，默认地址为 `/admin`。页面本身不展示敏感数据；读取状态、报告和触发分析的 API 都要求与 `/v1` 相同的 Bearer token。浏览器只把输入的 token 保存到当前标签页的 `sessionStorage`，不会放入 URL 或服务端日志。

## 配置

```yaml
admin:
  enabled: true
  path: /admin
  log_path: build/nohup.out
  expectations_path: conf/audit.yaml
  report_path: reports/audit-runtime.json
  refresh_interval: 0s
```

所有字段均为可选扩展，`enabled` 默认关闭，不改变原有 AiCon 接口。路径相对于 AinP 进程工作目录。`refresh_interval: 0s` 表示只允许人工刷新；对于数百 MB 或更大的日志建议保持该值，避免周期性全文件扫描与在线决策竞争 CPU/磁盘。确需定时刷新时可设置 `30m` 等 Go duration。

## 页面和 API

- `GET /admin`：内嵌页面，不依赖外部 CDN。
- `GET /admin/api/status`：进程运行时长、日志大小和更新时间、分析任务状态、最近错误。
- `GET /admin/api/report`：当前审计报告。
- `POST /admin/api/refresh`：异步启动一次流式日志分析；正在运行时返回 409。

面板将“运行期拒绝发牌”和“启动边界事件”分开显示。前者应为 0；后者表示进程重启时接入了缺少开局事件的旧手牌，只做黄色观察，并展示最多 20 条定位样本。`conf/audit.yaml` 的 `startup_boundary_grace_ms` 控制启动宽限期，默认 60 秒。

“迟到玩家事件流”表示同桌至少一个玩家已经记录 EndHand 后，另一玩家才开始向 AinP 发送该手的完整事件流。面板显示迟于结束的毫秒数、实际/建议动作和 advice 到广播的间隔。这类偏差不属于 AinP 实时执行偏差，但应检查调用方对应连接是否存在消息积压。

后台扫描不会阻塞 HTTP 请求线程。完成后先写临时文件再原子替换报告，页面轮询状态并自动读取新报告。可视化内容包括 HTTP/策略延迟、协议错误、延迟发牌、负 EV 与空气牌跟注、动作和街道分布、质量门禁、AiProfile 实际/目标 VPIP/PFR、主要错误消息及可疑跟注样例。特殊永不弃牌风格会列出全部输牌，点击单手的“查看”按钮会弹窗展示该手的阶段、动作、金额、牌力、胜率、底池赔率、跟注 EV 和策略规则。状态错误同时展示原始次数与按事件指纹去重后的唯一事件数，并保留最多 20 个状态错误、建议执行偏差及三条街高牌跟注的代表样例，因此通常无需重新读取 GB 级原始日志。

“特殊永不弃牌风格统计”只收集 `audit_exempt: true` 的 profile。其策略决策不会进入总体动作率、VPIP/PFR 偏差、负 EV 跟注、低胜率全下、高牌跟注或建议执行偏差门禁；HTTP、协议和状态错误仍照常统计。独立块展示手数/决策数、输赢和平局次数、胜率、净输赢、每手平均输赢、到达 flop/turn/river 的次数与比例，以及动作分布。输赢来自新增的 `hand_result` 结构化日志。

生产环境应通过防火墙、内网入口或反向代理限制 `/admin` 的网络访问，并通过 `AINP_AUTH_TOKEN` 设置强 token。后台报告只包含派生手牌类别和定位 ID，不包含原始底牌。
