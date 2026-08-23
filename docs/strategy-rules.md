# 第一版策略引擎

第 3 阶段规则策略位于 internal/strategy，并已由 EngineDecisionProvider 接入 `/v1/event`。ainp 内部完成状态转换、合法动作推导和结果格式转换，不修改 pokerbot。

## 输入

策略请求包含：

- 游戏类型：NLH、两种短牌规则、PLO4、PLO5 或 PLO6。
- 街道、位置、英雄牌、公共牌、对手牌或随机对手、死牌。
- 当前底池、需要跟注金额、英雄筹码、有效筹码和大盲。
- 已面对加注次数、是否为翻牌前主动方、强度等级和随机种子。
- 当前合法动作以及每个动作的最小、最大金额。
- personality_id、可选玩家画像，以及用于追踪的 request/decision/player/table/hand ID。

所有金额必须使用同一种筹码单位。Bet/Raise 的返回 Amount 表示目标动作金额，最终含义应在未来接入层与牌桌协议统一。

## 特征

引擎计算并返回：

- Equity 及算法元数据。
- Pot Odds 和 Call EV。
- 有效筹码 SPR。
- 当前牌型、空气牌、成牌、强成牌、听牌或成牌加听牌。
- 同花听牌、顺子听牌和顺子出牌数。

奥马哈特征同样强制使用两张手牌和三张公共牌；短牌特征使用对应牌组和牌型强弱规则。

## 翻牌前规则

UTG、MP、CO、BTN、SB、BB 使用不同的开池强度阈值。NLH 翻牌前使用169类起手牌对单个随机对手的标准化强度，不再把多人底池原始 Equity 直接套入单挑阈值；实际对手数量通过小幅 multiway penalty 单独收紧范围。后位范围更宽，机器人等级1～5与人格做小范围调整。

面对下注时结合 Pot Odds、位置阈值和 RaisesFaced：

- 仅有盲注、尚无人加注时，达到位置阈值主动加注，略低区间按配置决定跟注或弃牌。
- 面对加注时结合 Pot Odds、继续范围和加注次数；普通强牌跟注，超过再加注阈值才执行3-bet/4-bet。
- 额外加注逐次提高再加注门槛，但不再用大幅线性惩罚把 JJ 等强牌直接推入弃牌区。

这仍是对随机牌或显式已知牌的基础策略，不等价于基于对手 Range 的 GTO 翻牌前矩阵。

## 翻牌后规则

无人下注时：

- 强成牌或高 Equity：约 75% 底池价值下注。
- 听牌且 Equity 足够：约 50% 底池半诈唬。
- 主动方持普通成牌：约 50% 底池薄价值下注。
- 翻牌空气牌：按位置、等级和固定随机种子执行低频 34% 底池 C-bet。
- 其他情况过牌控制底池。

面对下注时：

- Equity 低于 Pot Odds 加风险边际时弃牌。
- 当 Call EV 为负且 `reject_negative_ev_calls` 开启时直接弃牌。
- 筹码差绝对值不超过 `1e-9` 时按 0 处理；只要合法动作包含 Check，最终动作保护和 HTTP 适配层都禁止返回 Fold。
- 空气牌/高牌需要额外越过按翻牌、转牌、河牌配置的跟注边际；此前已经用空气牌跟注的次数还会叠加惩罚，防止一路 bluff-catch 到摊牌。
- 转牌只有 4 张或更少补牌的弱听牌，跟注时额外叠加 `turn_weak_draw_call_margin`，避免把随机对手范围下的 A 高摊牌价值误当成干净听牌胜率。
- 河牌最终对子完全来自公共牌时按 bluff-catch 处理，额外叠加 `river_board_pair_call_margin`；若它同时是转牌顺子或同花听牌落空，再叠加 `river_missed_draw_call_margin` 和此前翻牌后跟注次数惩罚。对应保护弃牌不会被拟人化犯错改回跟注。
- 高 Equity 听牌可半诈唬加注，低 SPR 可全下。
- 强成牌执行价值加注或低 SPR 全下。
- 价格合适的听牌和普通成牌跟注。

公共牌不是独立的附加规则，而是直接进入牌力评估与 Equity 计算：英雄牌和当前全部公共牌共同决定 `hand_class`、听牌、Pot Odds、Call EV 和最终动作。NLH 的 AK 在低牌面没有配对时会被识别为 `air`；是否跟注取决于对随机/显式对手牌计算的 Equity 是否超过价格和上述空气牌边际。当前尚未按对手具体行动序列构建加权 Range，因此只按随机未知牌计算时可能高估 AK 高牌对真实下注范围的胜率；新增的逐街和连续跟注防护用于限制这一已知偏差，但不能替代后续 Range 模型。

相关开关均位于 `engine.strategy`：`flop_air_call_margin`、`turn_air_call_margin`、`river_air_call_margin`、`repeated_air_call_penalty`、`turn_weak_draw_call_margin`、`river_board_pair_call_margin`、`river_missed_draw_call_margin` 和 `reject_negative_ev_calls`。数值边际允许显式设为 0、布尔项允许设为 false，以保留旧策略；不改变任何 HTTP 请求或响应字段。

负 EV 和空气牌边际产生的保护性弃牌优先于拟人化。`bounded_loose_call` 不得把 `POSTFLOP_NEGATIVE_EV_FOLD` 或 `POSTFLOP_AIR_MARGIN_FOLD` 重新改成跟注；人格化只能在保护边界之外制造有界偏差。

每个分支返回稳定 RuleID 和 Tags，供决策日志、回放和后续规则效果分析。第 4 阶段会根据人格阈值与玩家画像做有界调整，原始牌局事实、Equity 与合法动作不被人格修改。

## 人格与对手调整

balanced、tight_passive、tag、lag、calling_station、tricky 六种内置人格调整开池/跟注/价值阈值、诈唬频率和下注尺度。固定种子下动作扰动与思考时间可复现；慢打和失误只在预设窄边界内发生，最终仍经过合法动作保护。

对手模型输入可包含 VPIP、PFR、3-bet、激进度、C-bet、fold-to-cbet、样本手数和类型。第一版按桌上快照做平滑平均：面对爱跟注玩家减少诈唬并放宽价值下注；面对紧手或高激进玩家小幅调整跟注边际。样本不足时保持 unknown 和保守先验。

## 合法动作保护

所有建议必须经过 actionGuard：

- 只返回请求明确提供的合法动作。
- Bet/Raise 金额钳制到 Min/Max，并按大盲向上取整。
- Check/Fold 金额固定为零，Call/AllIn 使用合法动作金额。
- 激进行为不可用时安全降级；通常不会把普通动作擅自升级为 AllIn。唯一例外是开启 `collapse_near_allin`、Call/Bet/Raise 后剩余筹码不超过 `near_allin_remaining_chips`，且请求明确将 AllIn 列为合法动作时，尾筹会归并为 AllIn。该约束在所有人格和等级的最终动作出口统一执行，包括永不弃牌及合法动作降级产生的 Call。
- 没有合法动作或金额范围错误时返回错误，不猜测动作。

## 当前限制

- 未实现按具体行动序列更新的手牌 Range；基础玩家统计不能替代 Range Equity。
- 未实现完整 GTO 表、求解器或强化学习。
- 听牌分类是基础规则，不计算被反超、反向隐含赔率或坚果听牌质量。
- PLO 尚未区分坚果听牌、重抽和牌张阻断效应。
- 当前合法动作来自标准无限注规则推导，特殊限注桌仍以 pokerbot 最终 ActionLimit 校验为准。

策略已可经 HTTP 工作，但仍应通过脱敏历史牌局回放和分级灰度验证，不能把第一版规则直接等同于成熟 GTO 策略。
