# 合法动作推导

AiCon v1 事件没有携带 pokerbot `NextAction` 的 ActionLimit，因此 ainp 根据事件状态推导动作，并仍由 pokerbot 使用真实 ActionLimit 做最后校验。

- 面对下注：fold；筹码足够时 call；达到最小完整加注时 raise；以及 allin。
- 无需跟注：check、满足最小下注时 bet，以及 allin。
- Call 金额为当前最高逐街投入减去英雄已有投入。
- Raise 最小支付金额为 ToCall 加 `max(LastFullRaise, bigBlind × min_raise_big_blinds)`。
- Bet/Raise 最大金额与 AllIn 金额为英雄剩余筹码。
- 短码无法完整跟注时只提供 fold/allin，不伪造普通 call。
- 唯一剩余对手已经全下时不能建立无人匹配的边池：面对下注只提供 fold/call，无需跟注时只提供 check，不提供 raise/bet/aggressive allin。

行动顺序按位置推导。两人桌是特殊情况：SB/Dealer 翻牌前先行动，BB 在 flop/turn/river 先行动；三人及以上翻牌后从 SB 或庄家左侧第一个仍可行动玩家开始。

策略返回仍必须经过 actionGuard：金额钳制到推导区间，Check/Fold 为零，非法激进行为只向安全动作降级。pokerbot 的真实 ActionLimit 是最终事实源；若特殊桌型的服务端限制与标准无限注规则不同，应扩展版本化合法动作字段，而不是放宽 Guard。
