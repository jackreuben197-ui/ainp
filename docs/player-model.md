# 玩家模型

internal/opponent.Tracker 提供第 4 阶段的进程内基础玩家统计。Observe 使用 observation_id 幂等更新，并按 hand_id 计数独立牌局；Snapshot 返回带先验平滑的 Hands、VPIP、PFR、ThreeBet、Aggression、CBet、FoldToCBet 和 Archetype。

类型包括 unknown、nit、calling_station、lag、tag、loose_passive、balanced。少于 20 手保持 unknown，避免用极小样本强分类。默认最多保留 10,000 名玩家，每名玩家仅保留最近 256 个事件 ID 和 256 个手牌 ID 用于去重；超过上限淘汰最久未更新的玩家，长期累计计数不受单个玩家去重窗口裁剪影响。

当前模型没有持久化，也不构造逐街 Range。进程重启或玩家被淘汰后统计会回到先验。正式接入事件流时应：

1. 从规范化行动生成唯一 observation_id，并正确标注每种 opportunity。
2. 将快照异步持久化到现有存储，在启动时按需回填。
3. 按游戏类型、人数和盲注级别分桶，避免混合不同生态。
4. 只向策略提供牌桌内可合法观察的数据。
