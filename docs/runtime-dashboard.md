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

“迟到补发玩家事件流”包含两种情况：同桌至少一个玩家已经记录 EndHand 后才开始补发，或牌局尚未结束但其他玩家流的同桌事件序号已经明显前进后才从 StartHand 开始补发。面板分别显示两类数量、开始延迟、开始时同桌最大序号、实际/建议动作和 advice 到广播的间隔。这类偏差不属于 AinP 实时执行偏差，但应检查调用方对应连接是否存在消息积压。

后台扫描不会阻塞 HTTP 请求线程。完成后先写临时文件再原子替换报告，页面轮询状态并自动读取新报告。可视化内容包括 HTTP/策略延迟、协议错误、延迟发牌、负 EV 与空气牌跟注、河牌低边际一对跟注、动作和街道分布、质量门禁、AiProfile 实际/目标 VPIP/PFR、主要错误消息及可疑跟注样例。河牌跟注统计同时给出旧日志可计算的宽口径，以及新日志中的公共牌对子、同花落空、顺子落空、任一听牌落空和连续追牌精确口径；没有新版结构化字段的旧日志不能反推精确口径。翻前大额跟注面板展示新版 `preflop_large_call_outside_range` 保护遗漏、`PREFLOP_PROFILE_LARGE_CALL_FOLD` 保护命中，以及旧日志中“面对至少两次加注仍然跟注”的宽口径候选；旧日志缺少 BB 阈值和派生范围字段，候选不等同于确定缺陷。Underpair 面板分别展示新版 `pocket_pair_under_board` 精确跟注、`POSTFLOP_UNDERPAIR_FOLD` 保护命中，以及旧日志中“转河口袋对子最终仍为一对并跟注”的候选；旧候选无法从脱敏日志恢复公共牌，因此可能包含少量 overpair。特殊永不弃牌风格会列出全部输牌，点击单手的“查看”按钮会弹窗展示该手的阶段、动作、金额、牌力、胜率、底池赔率、跟注 EV 和策略规则。状态错误同时展示原始次数与按事件指纹去重后的唯一事件数，并保留最多 20 个状态错误、建议执行偏差及三条街高牌跟注的代表样例，因此通常无需重新读取 GB 级原始日志。

实际 VPIP/PFR 会始终显示。`conf/audit.yaml` 的 `enforce_profile_rates` 控制它们是否作为质量门禁：只有各 profile 获得近似均匀随机手牌时才应开启。若调度按预获取牌型将最大牌定向赋给特殊风格，其他风格的样本分布会系统性偏弱，观察频率不能直接与配置的起手牌范围宽度比较。

“特殊永不弃牌风格统计”只收集 `audit_exempt: true` 的 profile。其策略决策不会进入总体动作率、VPIP/PFR 偏差、负 EV 跟注、低胜率全下、高牌跟注或建议执行偏差门禁；HTTP、协议和状态错误仍照常统计。独立块展示手数/决策数、输赢和平局次数、胜率、净输赢、每手平均输赢、到达 flop/turn/river 的次数与比例，以及动作分布。输赢来自新增的 `hand_result` 结构化日志。

生产环境应通过防火墙、内网入口或反向代理限制 `/admin` 的网络访问，并通过 `AINP_AUTH_TOKEN` 设置强 token。后台报告只包含派生手牌类别和定位 ID，不包含原始底牌。
