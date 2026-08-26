# 人格与拟人化模型

第 4 阶段实现位于 internal/personality，并在 internal/strategy 中应用。人格只改变合法策略边界、下注尺度、受控随机行为和思考时间，不改变手牌、公共牌、Equity 或发牌结果。

内置 personality_id：balanced、tight_passive、tag、lag、calling_station、tricky。每个不可变 Profile 包含开池/跟注/激进/价值阈值偏移、诈唬与下注倍率、有界失误率、慢打率和思考时长上下限。未知 ID 返回错误，不静默猜测。

拟人化满足以下约束：

- 同一输入和 Seed 产生相同动作与时间，便于回放。
- 失误仅限边缘牌宽跟、边缘下注遗漏或非强牌被动处理。
- 慢打只发生在高 Equity 牌，并只降为合法的 check/call。
- 所有扰动之后重新经过 Legal Action Guard。
- ThinkTime 是可复现的建议时长；`apply_think_time` 开启时 HTTP provider 在返回 advice 前等待，关闭时只记录建议时间。

调用方通过每手 `start_hand_extended.payload.ai_profile` 选择风格。`personality.profiles` 可把它映射为人格和等级；映射只保存在该手牌 State 中，不需要为不同风格部署多个 AinP 实例。调度系统可以在下一手切换风格，但不建议在缺少实验标记时随机切换，否则同一玩家的长期对手画像会混合多种行为。

生产配置包含 AiCon `fpch_profile.csv` 列出的全部名字。每个复合风格显式配置 `target_vpip` 和 `target_pfr`。NLH 翻前把全部 1,326 种具体两张组合按预计算胜率排序，第一次主动入池时，PFR 区间取最强的前 `target_pfr`，VPIP 区间取最强的前 `target_vpip`；PFR 区间加注，二者之间跟注/跛入，其余弃牌。具体花色组合用于稳定拆分同一 169 手牌类别，误差上限约为一个组合（1/1326）。已经入池后恢复基于 Equity、Pot Odds 和加注次数的继续策略，但 VPIP-only 区间不会在后续动作越界变成 PFR。目标范围开启后不会应用翻前“有界失误”，避免拟人随机扰动破坏长期比例。

映射为：`FPCH_default` 32/16、balanced/L5；`FPCH_30_15` 30/15、tag/L4；`FPCH_39_14` 39/14、balanced/L3；`FPCH_54_11` 54/11、calling_station/L2；`FPCH_60_5` 60/5、calling_station/L1；`FPCH_60_10` 60/10、calling_station/L2；`FPCH_90_5` 90/5、calling_station/L1。显式 VPIP/PFR 决定 NLH 翻前入池与加注范围，`level` 和 `personality` 继续控制翻后阈值、诈唬、慢打、失误和思考时间，不再重复修改目标翻前比例。各 `_S1`、`_S2` 名称继承基础风格的目标、强度和人格，并通过 `postflop_sizings` 限制翻后下注和加注的底池比例档位；原文件将后缀定义为翻牌后下注尺度变体，而不是新的强度等级。

`FPCH_100_50` 是与正常强度体系分离的特殊模式。它 100% 主动入池，翻前按配置概率 Raise，否则 Call/Check；翻后按配置概率优先 Bet/Raise，无法合法进攻时由动作保护降级为 Call/Check/All-in。该模式忽略 Equity、EV 和普通资金保护且永不主动 Fold，用于明确要求的特殊陪打场景，不能作为盈利型策略。它的策略异常不混入正常机器人门禁，而由 Admin 独立显示手数、动作、到达各轮次比例和 end_hand 净输赢。

VPIP/PFR 是按“每名玩家、每手牌至少一次主动投入/加注”统计，不是动作次数占比。大盲免费过牌不计 VPIP；全桌弃到大盲会使实战 VPIP 低于范围目标，这是标准统计口径。面对加注、短码全下和合法动作限制也会带来小幅环境偏差。当前显式范围只应用于 NLH；PLO/短牌仍使用原有胜率阈值策略，不能拿 NLH 的 FPCH 数字作为同义目标。

生产日志记录调用方 `ai_profile`、实际 `personality_id`、`strategy_level`、`target_vpip`、`target_pfr`、humanized、rule_id、tags 和 think_time_ms。`logaudit` 按玩家和手牌去重后输出每个 profile 的实际 VPIP/PFR。

`FPCH_90_5` 使用独立的翻后资金保护：底池达到配置的 20BB 后，估算 Equity 低于 58% 时不再扩大底池并在面对下注时弃牌。该参数经过固定牌序 A/B 校准；更激进的 12BB/68% 保护会错误切掉正 EV 跟注，因此没有采用。目标是把 90% 入池产生的边缘损失限制在小底池，把大额投入集中到优势牌。盈利仍然依赖对手和抽水，必须同时查看生产日志的 BB/100、对手分层和大底池样本，不能把离线基准解释为无条件盈利承诺。
