# 调用与决策日志

服务使用 slog 向标准输出写结构化 JSON。每次 HTTP 调用都会生成 http_access，包含 request_id、method、path、route、status、response_bytes 和 latency_us；包括 health、鉴权失败、参数错误、208 重试和成功请求。

/v1/event 的可解码请求还会生成 decision_event，outcome 为 applied、duplicate、rejected 或 failed。字段包括稳定 decision_id、event_fingerprint、错误码与错误消息、玩家/房间/牌桌/手牌/序号/命令、provider、建议、实际动作偏差 deviation 和延迟。同一事件重试的 decision_id 与 SHA-256 指纹保持一致，可与 208 日志关联。无法解码的请求生成 event_decode_error，鉴权失败生成 auth_rejected；真实引擎失败及 Mock 降级分别生成 engine_decision_failed/engine_decision_fallback。

策略引擎通过 NewEngine(..., WithLogger(logger)) 开启 strategy_decision，记录追踪 ID、policy_version、调用方 ai_profile、profile_source、实际 personality_id、strategy_level、target_vpip、target_pfr 和 postflop_sizings、玩法/街道/位置、NLH 169类英雄手牌（如 JJ/AKs，不记录具体花色）、活跃对手数、已面对加注数、英雄本手翻牌后跟注次数、对手类型和样本数、底池/跟注/有效筹码、Equity 算法与样本、牌型分类、动作/金额、RuleID/Tags、Pot Odds/Call EV/SPR、拟人标记、建议思考时间和计算延迟。河牌额外记录 `pair_from_board_only`、`missed_flush_draw` 和 `missed_straight_draw` 三个派生布尔值，用于识别“听牌落空、最终只使用公共牌对子仍跟注”，但仍不记录具体牌面。`logaudit` 按玩家、牌桌和手牌去重计算每个风格的实际 VPIP/PFR。

每个成功应用的 `end_hand` 额外记录一条 `hand_result`，包含英雄、牌桌/手牌、AiProfile、到达的最终轮次和 end_hand 中的英雄 profit。`audit_exempt` 特殊 profile 使用这些日志计算独立输赢及轮次统计；普通 profile 仍按原有门禁分析。

`cmd/logaudit` 将 Game logic error、Wrong seq_num、动作类型 deviation 和可免费看牌却弃牌作为独立的零容忍门禁，并按 `ai_profile|personality_id|strategy_level` 输出翻牌前与面对翻牌后下注的分层指标。它还检查延迟 DealCards 的拒绝、漏 advice、P95 响应，负 EV 跟注、低边际高牌跟注、连续多条街高牌跟注、低胜率主动全下和河牌高胜率过牌。河牌一对跟注另按 `min_river_pair_call_equity_edge` 输出宽口径低边际数量；带新版派生字段的日志会独立统计公共牌对子跟注、顺子/同花听牌落空跟注，以及此前已在翻牌后跟注过的连续追牌样本，不再依赖可能被随机对手范围抬高的 Equity 边际。该观察项暂不作为质量门禁，需有足够样本或 replay 后再决定是否收紧策略。耗尽筹码的跟注即使协议动作表示为 All-in，也不计入主动全下。实际 VPIP/PFR 目标偏差只有 `enforce_profile_rates=true` 时参与门禁；按牌型定向分配 BotStyle 时应关闭。免费看牌弃牌报告会保存最多 20 条可定位的决策样例。旧版 AK 字段只为历史报告兼容；通用检测覆盖所有当前仅高牌且无基础顺子/同花听牌的手牌。三条街高牌跟注本身是观察项，只有负 EV 或河牌安全边际不足才默认判失败。

动作执行偏差样例会关联同一牌手/牌桌/手牌最近一次 advice，输出建议来源事件、建议 decision_id 和从建议到服务器实际广播的毫秒数。该字段可直接识别动作延迟或服务器超时，不再需要手工扫描前后日志。

同桌其他玩家已经记录 `end_hand`，或者同桌仍在进行但事件序号已经明显前进，超过 `late_stream_after_end_threshold_ms` 后某玩家才开始发送相同 table/hand 的 StartHand，都表示该玩家的整条历史事件流迟到。其后出现的 advice/Fold 不代表 Call 被实时转换成 Fold，审计按 `after_end`、`after_table_progress` 单列数量、延迟和开始时同桌序号，并从实时 `action_type_deviations` 门禁排除。该分类只能证明事件流迟到；具体积压位于服务器投递、网络连接还是 pokerbot 消息处理，需要结合对应 pokerbot 日志判断。

审计器对大日志执行一次流式扫描，报告同时保存：按事件指纹去重的状态错误数及最多 20 个代表样例、建议与执行偏差样例、三条街高牌跟注逐街的胜率/底池赔率/EV。后续定位应优先读取这份小型 JSON，不需要重新逐条展开原始日志；样例含决策 ID 和牌局定位字段，可用于回放，但不含原始牌面。

被拒绝的延迟 DealCards 只计入 `rejected_deal_cards` 及其实际协议错误，不再重复计入“已成功应用但未返回 advice”；这样后台能区分重复发牌/错序与真实漏建议。`/admin` 可异步执行同一套审计并可视化报告，说明见 [runtime-dashboard.md](runtime-dashboard.md)。

AinP 重启后，仍在进行中的牌局可能继续发送 Action、DealCards 或公共牌事件，但新进程没有该手的 `start_hand_extended`。`logaudit` 使用 `startup_boundary_grace_ms` 将宽限期内这种明确的缺失开局错误单列为 `startup_boundary_errors`；其中的 DealCards 计入 `startup_rejected_deal_cards`，不计入运行期 `rejected_deal_cards` 零容忍门禁。宽限期后出现相同问题仍按运行期错误处理。

日志禁止记录认证 token、query access_token、完整请求体、生产密钥或不可见牌。部署时应由 journald、容器 runtime 或日志采集器负责轮转；4CPU/8GB 单机建议本地保留 3～7 天并限制总磁盘量，分析明细异步发送到 ClickHouse 或现有日志平台。用户要求完整调用审计时不可采样 http_access；如日志量过大，应缩短本地留存而不是漏记请求。

当灰度开启时，`gray_decision` 记录 mode、稳定路由、基准/候选版本、两侧建议、动作是否一致和下注差值。完整请求不会写普通运行日志；只在 `phase5.replay.enabled` 开启时写入受权限保护的回放 JSONL。
