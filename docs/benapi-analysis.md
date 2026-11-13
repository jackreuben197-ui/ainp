# BenAPI 全量日志与 AinP 对比分析（2026-08-28）

## 结论摘要

本轮分析了 `build/benapi` 下 569 个完整文件、52,031,663,803 bytes（约 48.46 GiB），覆盖 2026-08-22 至 2026-08-25、40 个 pokerbot 实例。去除跨目录重复记录后，得到 2,113,585 条普通 Hold'em 决策、1,211,422 条结束回调，以及 66,875 条 AOF 响应。完整机器报告见 `reports/benapi-analysis-20260828.json`。

最重要的结论如下：

1. AinP 当前最接近 BenAPI 的仍不是更松的 profile，而是 `FPCH_30_15`。8,000 条分街道样本的一致率为 60.33%，按日志真实街道占比加权约 64.02%；`FPCH_default` 分别为 60.43% 和 63.77%。54/11、60/10、60/5、90/5 依次降低。因此新增三倍样本后，仍不能支持“把 AinP 整体调松”。
2. BenAPI 明确且稳定地使用混合策略：47.55% 的 Hold'em 决策有两个以上正概率动作，最终选择有 17.07% 不是最高概率动作。每日 mixed rate 仅在 47.32%～47.83% 波动，说明不是少数机器或日期造成的偶然现象。
3. AinP 与 BenAPI 最大的结构差异不是单一 VPIP/PFR，而是翻后动作混合、位置/straddle、对手长期信息、下注尺度和一些边界保护。直接照抄一个动作或一个阈值不可靠。
4. 数据中 84.91% 的 Hold'em 决策处在有 straddle 的牌局，20.43% 的所有决策由 STR 座位执行；AinP 当前把 `st` 排除后回退为 MP。这是有充分且跨日稳定样本支持的 AinP 建模缺口。
5. 6.10% 的 Hold'em 请求带新手盲/补盲，每日为 5.97%～6.21%。pokerbot 的 AinP 适配器能发送普通 ante/blind/straddle，但没有消费 `Progress.Post`；仅靠 `stack_after`/合法动作只能部分修正英雄筹码，不能完整修正所有玩家贡献和底池。
6. pokerbot 的 BenAPI AOF 回调存在高风险下标错位：请求玩家经过旋转和过滤，回调却按原始 `DealOrder[i]` 回填动作和手牌。日志中已经出现请求英雄手牌 `AhTd`，回调却把 `AhTd` 挂到另一名玩家的实例。这会污染 BenAPI AOF 回调数据，应优先修 pokerbot，而不是据此调整 AinP。
7. 不能从这些日志证明 BenAPI 比 AinP 盈利更好。1,206,881 条可结算回调均值为 +0.0114 BB/手，但朴素 95% 区间为 [-0.0264, +0.0491]，跨过 0；每日均值从 -0.0137 到 +0.0644，40 个实例从 -0.2959 到 +0.2904。日志没有随机化 AinP 对照组，回调收益只能作为异常线索，不能作为引擎优劣的因果结论。

## 可重复执行

分析器现在支持目录递归、`.part` 标记、跨目录去重、日期/机器/实例分组、错误码归因、AOF、概率、延迟、尺度、收益置信区间和多 profile 对比：

```shell
go run ./cmd/benapianalyze \
  -input build/benapi \
  -config conf/config.yaml \
  -profiles FPCH_30_15,FPCH_default,FPCH_39_14,FPCH_54_11,FPCH_60_5,FPCH_60_10,FPCH_90_5 \
  -max-compare 8000 \
  -equity-samples 1000 \
  -max-examples 120 \
  -output reports/benapi-analysis-20260828.json
```

旧命令的 `-input 'build/benapi/*'` 只能命中顶层四个旧文件，不能读取新下载的多层目录。新默认值是 `build/benapi`，目录会递归扫描。

## 数据质量和去重

- 原始 Hold'em `NextAction Success`：2,127,311；精确重复 13,726；去重后 2,113,585。
- 原始 Hold'em 回调请求：1,219,277；精确重复 7,855；去重后 1,211,422。
- 569 个文件均已下载完成，没有 `.part` 或 0-byte 文件；按文件名覆盖 2026-08-22 至 2026-08-25。
- 共记录 70 个坏行，主要是 100 MiB 日志轮转边界的截断 JSON；每个文件和行号均保存在报告中。
- 顶层四个旧文件与递归目录有重叠。去重避免它们重复抬高动作、VPIP/PFR 和对比样本权重。
- 回调收益统计跳过 4,541 条无法用 `current_player_id` 对齐 payoff 或 BB 非法的记录；报告显式记录该数量。

## Hold'em 接口稳定性

普通决策请求 2,131,632，解析到响应 2,131,537，相差 95 条，约为 0.0045%，符合日志切片/在途请求的量级。

- 成功 2,127,311，code 105 为 4,226，响应失败率约 0.198%。每日在 0.177%～0.226% 之间，没有单日故障尖峰。
- 3,982 次是 `valid action must be ...`，脚本归类为 `invalid_legal_action_state`。
- 152 次是历史 raise 超过 max raise。
- 其余 92 次主要是当前玩家/动作序列玩家 ID 错位，另有 2 次 `game over` 和 1 次公共牌数量错误。

结束回调请求 1,219,277、响应 1,219,227，相差 50 条；成功 1,206,646，code 105 为 12,581，失败率约 1.032%，每日在 1.007%～1.057%。

- 7,714 次消息只有 `'0'`，从上下文推断是玩家 0 查找失败，但 BenAPI 错误文本本身信息不足。
- 3,844 次是非法动作状态。
- 916 次是 raise 超过 max raise。
- 其余包括 21 次 `game not finished`、16 次响应解析错误和少量玩家 ID 错位。

这些错误跨四天稳定出现，大部分说明 pokerbot 构造出的全量快照与 BenAPI 状态校验不一致，不宜简单归责为 BenAPI 策略能力。AinP 的严格事件状态、`seq_num`、`next_player_id`、`stack_after` 和服务端 `legal_actions` 对这类错位更容易定位；但 AinP 客户端在超时后的恢复仍需加强，见后文。

## Hold'em 行为画像

### 样本构成

- 街道：preflop 1,308,969（61.93%）、flop 396,619（18.77%）、turn 246,830（11.68%）、river 161,167（7.63%）。
- 风格：`shark` 1,883,152、`raw` 123,064、`shark2` 107,369。
- 2～8 人桌；没有 9 人桌。
- 全部为 `game_type=holdem`、`anti_cheat_mode=false`、`use_diff_raise_logic=true`，UUID 列表完整。
- straddle 非零 1,794,699（84.91%）；新手盲 129,013（6.10%）。STR 座位本身有 431,807 条决策（20.43%）。

### 实测 VPIP/PFR

按 UID + game_id 去重后：

| BenAPI 风格 | 手牌数 | VPIP | PFR |
|---|---:|---:|---:|
| raw | 64,577 | 37.28% | 12.81% |
| shark | 1,073,390 | 35.86% | 23.52% |
| shark2 | 62,589 | 37.23% | 22.18% |

`shark2` 没有表现为明显更松，但其最高概率动作命中率更低（76.98%，`shark` 为 82.62%），更像改变动作混合和调整层，而不是简单扩大翻前范围。

### 跨日期稳定性

| 日期 | 决策 | Mixed | 选择 argmax | Straddle | 新手盲 | 可结算回调 | 平均 BB/手 |
|---|---:|---:|---:|---:|---:|---:|---:|
| 2026-08-22 | 332,458 | 47.76% | 83.00% | 83.81% | 6.14% | 190,603 | +0.0044 |
| 2026-08-23 | 478,397 | 47.32% | 83.11% | 85.56% | 6.21% | 274,325 | +0.0644 |
| 2026-08-24 | 633,626 | 47.33% | 82.92% | 85.48% | 6.14% | 361,957 | +0.0013 |
| 2026-08-25 | 669,104 | 47.83% | 82.79% | 84.45% | 5.97% | 379,996 | -0.0137 |

行为指标跨日很稳定，而收益正负波动明显。由此可以提高对“BenAPI 有混合策略、STR/补盲覆盖高”的置信度，但必须降低对累计盈利排名的置信度。

### 混合策略与调整层

- 2,113,585 条去重决策全部带 `action_probs`。
- 1,005,068 条有两个以上正概率动作（47.55%）；flop/turn/river 分别约 71.02%/72.93%/64.34%，preflop 为 33.59%。
- 最终动作是 argmax：1,752,869（82.93%）；不是 argmax：360,716（17.07%）。
- 所选动作平均概率 76.69%。
- 原始 API 响应中 `db_tools` 795,706、`normal` 1,331,605；所有 `db_tools` 记录的调整后概率都与 `origin_action_probs` 不同，即约 37.40% 成功响应经过额外概率调整。
- `use_x1_action` 为 true 的记录为 0。

`db_tools` 能证明存在额外调整层，但日志不能证明它具体使用了 UUID、盈利、摊牌或哪种模型，因此报告不把“可能的长期剥削”写成确定事实。

### 延迟

`decision_time` 字段原值：均值 1661、P50 1500、P90 3000、P95 3375、P99 4500、最大 9000。BenAPI/pokerbot 代码没有声明该字段单位；结合 10 秒 HTTP timeout 看，很可能是毫秒或期望思考延迟，而不能直接当作纯模型计算耗时。

### 动作与尺度

- Fold 903,852；Check 392,619；Call 306,542；Bet 203,686；Raise 306,886。
- 510,572 次主动动作中，按返回 amount 达到当前 stack 判断的 all-in 为 40,308（7.89%）。
- 能识别的标签主要是 0.5 pot 49,763、1.0 pot 62,576、2.0 pot 5,756、all-in 9,572；382,905 次未带 `raise_action`，因此不能只按标签反推完整尺度表。

### 回调收益（只能描述，不能判定引擎强弱）

1,206,881 条可对齐回调合计 +13,743.27 BB，平均 +0.0114 BB/手；单手 payoff 标准差高达 21.16 BB，朴素 95% 区间为 [-0.0264, +0.0491]。分风格为：raw -0.0646 BB/手（区间 [-0.2109, +0.0817]）、shark +0.0268（[-0.0135, +0.0672]）、shark2 -0.1757（[-0.3242, -0.0273]）。

该结果仍不能用于得出 `shark > raw > shark2`：机器人面对的牌桌、人数、盲注、对手和风格分配均未随机控制；多个机器人可能互相对打；40 个实例均值从 -0.2959 到 +0.2904 BB/手；仅 99 手报告非零 rake，说明 rake 数据也不完整。上述区间还把相关牌局当作独立样本，正式检验应按桌/机器人/时间块做 cluster bootstrap。正确做法是同桌随机分流或 shadow/canary A/B，并统一按实际 rake、对手类型和牌桌条件结算。

## AinP 动作对比

每条街固定种子抽 2,000 条，共 8,000 条；所有 profile 使用当前分支的真实策略代码和当前配置。旧分析器遗漏的 `preflop_reraise_range_factor`、大额翻前跟注保护、underpair margin 等已补齐；旧分析器“有效筹码总是英雄 stack”的错误也已修复。

| AinP profile | 分层一致率 | 按真实街道占比加权 | Preflop | Flop | Turn | River | AinP 动作在 BenAPI 候选中有正概率 |
|---|---:|---:|---:|---:|---:|---:|---:|
| FPCH_30_15 | 60.33% | 64.02% | 68.00% | 58.60% | 53.35% | 61.35% | 65.66% |
| FPCH_default | 60.43% | 63.77% | 67.45% | 58.40% | 54.35% | 61.50% | 66.44% |
| FPCH_39_14 | 59.65% | 62.01% | 64.75% | 58.05% | 54.05% | 61.75% | 65.63% |
| FPCH_54_11 | 57.24% | 56.42% | 55.90% | 57.45% | 54.50% | 61.10% | 64.15% |
| FPCH_60_10 | 56.14% | 53.88% | 51.90% | 57.15% | 54.45% | 61.05% | 63.21% |
| FPCH_60_5 | 56.21% | 53.05% | 50.20% | 57.50% | 55.15% | 62.00% | 62.66% |
| FPCH_90_5 | 53.61% | 42.86% | 32.00% | 60.65% | 58.65% | 63.15% | 61.44% |

一致率是“像不像”，不是“谁更好”。BenAPI 自身会随机抽非 argmax 动作，所以即使策略分布完全相同，单次动作一致率也不会到 100%。

### `FPCH_30_15` 的主要差异

- Flop：BenAPI Fold 的 324 个样本中，AinP 203 次 Call、4 次 Raise，仅 117 次 Fold；新增样本仍稳定指向 AinP flop 继续范围偏宽。
- Turn：BenAPI Check 的 870 个样本中，AinP 408 次 Bet；BenAPI Bet 的 502 个样本中，AinP 218 次 Check。AinP 更接近阈值驱动的主动下注，BenAPI 更常在 bet/check 间混合。
- River：BenAPI Call 的 202 个样本中，AinP 116 次升级为 Raise；BenAPI Check 的 971 个样本中，AinP 330 次 Bet。应专项检查河牌薄价值、诈唬和 raise 上限。
- Preflop：BenAPI Fold 的 1,182 个样本中，AinP 146 次 Call、2 次 Raise；BenAPI Raise 的 439 个样本中，AinP 155 次 Call、83 次 Fold。其余差异集中在面对 raise 的 Call/Raise/Fold 边界。

翻后结论置信度低于翻前，因为 BenAPI 的 `action_sequance` 没有街道分隔，无法精确恢复 AinP 当前街 raises、LastFullRaise、英雄此前翻后 call 次数和服务端合法按钮。分析器选择把无法恢复的当前街 raises/此前 call 设为 0，并在报告中显式记录，不用全手累计值冒充当前街状态。

### 同动作时的尺度差异

`FPCH_30_15` 中有 1,094 个双方选择同一主动动作且 pot 可用的样本：BenAPI 尺度/Pot 中位数 0.50，AinP 为 0.762；仅 48.54% 的样本相差不超过 0.25 pot。该结果继续提示 AinP 尺度偏大，但 raise amount 是否包含 to-call 在 BenAPI 文档和 pokerbot 注释中存在矛盾，因此应先用服务器实际 ActionLimit/贡献增量做一次端到端校验，再改生产尺度。

## AOF 分析

- 请求 66,880，响应 66,875；成功 66,820，code 105 为 55（34 次 hero_index mismatch、21 次 unknown error）。
- 动作：Push 36,735（54.98%）、Fold 30,085（45.02%）；版本全部为 `3.0.3`。
- Push EV：均值 -0.517、中位数 -0.776、P90 1.906；Fold EV：均值 -1.036、中位数 -1.029。负值仍选择 Push 并不异常，应该比较 Push EV 与 Fold EV，而不是与 0 比。
- AOF 回调请求 68,673、响应 68,668，成功 68,646，失败 22。

BenAPI 的 AOF 返回可解释的 Push/Fold EV，这是当前 AinP 没有的专用能力。AinP 在 AOF 房间仍使用普通 NLH 策略，只靠服务器 legal actions 把结果约束为 Fold/All-in；66,820 条成功样本进一步强化了这一能力差距。若 AOF 要迁移到 AinP，应实现独立 Push/Fold EV 模型，不能只复用普通现金桌阈值。

### pokerbot AOF 回调错位（应先修）

`engine/benapi/benapi_aof.go` 的 `buildAofRequest` 会从首个行动者开始旋转玩家，并过滤此前 force-allin 玩家；`buildAofCallbackRequest` 随后却：

1. 用原始 `DealOrder` 下标建立 `playerActionMap`；
2. 遍历旋转/过滤后的 `aofReq.Players` 时仍取 `eng.handInfo.DealOrder[i]`；
3. 用这个旧 seatID 回填手牌。

因此动作、seat、玩家 ID、手牌可能错配，hero_index 也可能失效。修复时必须让 request builder 同时返回“API player index -> 原始 seatID/UID”的显式映射，callback 全程复用该映射，并增加旋转、过滤英雄、多人 all-in 的测试。

## AinP 与 BenAPI 的能力对照

| 维度 | AinP | BenAPI/当前 pokerbot 接入 |
|---|---|---|
| 状态 | 严格递增事件、错序检测、幂等、回放 | 每次全量快照，适配器更容易构造出内部不一致 |
| 合法动作 | 可接收服务端 legal_actions，并有最终约束 | 返回后由 pokerbot 二次映射；映射失败目前会静默退成 Fold |
| 可解释性 | RuleID、Equity、PotOdds、EV、SPR、特征、审计 | Hold'em 有动作概率/来源但没有规则解释；AOF 有 Push/Fold EV |
| 随机性 | 有人格/人性化，但核心仍偏阈值和确定性 | 明确输出并抽样完整混合概率 |
| 对手模型 | 动作统计，单进程内存，未持久化；不从输赢/摊牌更新 range | UUID + callback + `db_tools` 调整入口较完整，具体算法未知 |
| 公平性 | 固定只用可见牌 | 日志均 `anti_cheat_mode=false`，但接口暴露开关 |
| 玩法 | 多牌种 equity/通用协议；AOF 无专用 EV | Hold'em、Bomb、AOF，以及未在本批启用的 MTT/Spin/Squid 字段 |
| 运维 | 自建、可 gray/shadow、完整 replay/audit/admin | 外部黑盒；有 code/decision_time，但部分错误文本差 |

## BenAPI/现有接入相对 AinP 的不足

1. **状态一致性与诊断较弱。** BenAPI 的全量快照方便无状态调用，但 4,226 次普通决策失败和 12,581 次 callback 失败持续暴露快照内部矛盾；`'0'`、`unknown error` 等文本无法直接定位玩家、街道和字段。AinP 的事件序号、审计字段和 replay 更适合复现问题。
2. **Hold'em 可解释性不足。** `action_probs`、`origin_action_probs` 和 `db_tools` 能说明最终分布，但不能说明具体牌力、pot odds、EV、触发规则或对手调整来源。AinP 的 RuleID/Equity/PotOdds/Features 更适合调试和合规审计。
3. **下注金额语义不够明确。** 日志和 pokerbot 注释无法完全确定 raise amount 是增量还是目标投入；75.00% 左右的主动动作没有 `raise_action` 标签。直接拿返回 amount 校准 AinP 尺度有较大误改风险。
4. **收益闭环字段名义完整、实际不完整。** callback 的 payoff/rake/showdown 字段覆盖率均为 100%，但 120 万可结算手中只有 99 手 rake 非零；无法据此可靠比较净 BB/100 或费用敏感策略。
5. **外部黑盒与版本控制能力较弱。** Hold'em 响应没有稳定可追踪的模型/策略版本字段；AOF 只有固定 `3.0.3`。AinP 能把 policy version、profile、gray/shadow、回放结果统一进审计链路。
6. **AOF 接入数据被本地映射 bug 污染。** 这是 pokerbot 接入不足，不一定是 BenAPI 服务不足，但会让 callback 学习/评估链路收到错误玩家、动作和手牌。

BenAPI 仍有 AinP 当前明确欠缺的能力：完整动作概率分布、成熟的 AOF Push/Fold EV、基于 UUID/callback 的额外调整入口，以及较稳定的跨日行为输出。这些能力适合作为 AinP 离线校准和产品需求参考，不适合不经验证地直接复制。

## 是否优化：建议清单

| 优先级 | 项目 | 是否建议 | 理由与实施边界 |
|---|---|---|---|
| P0 | 修复 pokerbot AOF callback 玩家映射 | **是，立即** | 已有代码和日志双重证据表明回调手牌/动作会错位，会污染 BenAPI AOF 数据；这是接入 bug，不是策略调参。 |
| P0 | BenAPI 适配器非法动作不要静默返回 Fold | **是，若继续使用 BenAPI** | `convertBenapiAction` 找不到 action limit 时返回 Fold/0，外层仍按成功处理。应返回 error 或采用与 AinP 相同的 Check→Fold→Call 安全 fallback，并记录原建议和合法按钮。 |
| P1 | AinP 增加 STR 一等位置 | **是** | 84.91% 样本有 straddle，20.43% 决策来自 STR；当前回退 MP 会混淆已投入 blind、行动顺序、翻前范围和 3-bet/call 阈值。需同时改 position 类型、state role、range/测试/审计。 |
| P1 | 显式支持 dead blind / beginner post | **是** | 6.10% 决策命中且每日稳定；已有真实业务覆盖，不再只是条件建议。增加通用 forced-post 事件/字段，而不是硬编码“新手盲”；必须计入 Pot、玩家 contribution、stack、位置和合法动作。 |
| P1 | AinP 客户端同事件重试与整手失同步保护 | **是** | 服务端已支持相同事件 208 幂等，但 pokerbot `sendEvent` 在 timeout/网络错误后不重试；若服务端已应用，下一事件会以旧 seq 发送不同 payload，导致整手级联错序。应对同一序号/同一 body 做有限重试，失败后标记该手 sync-broken 并全手安全 fallback。 |
| P1 | 建立翻后混合策略离线层 | **是，先离线/gray** | BenAPI 翻后约 64%～73% 决策为混合策略；AinP Turn check→bet、River call→raise 差异明显。先让规则输出候选动作、权重和禁用原因，用 replay 校准；不要直接随机化受保护 Fold 或负 EV Call。 |
| P1 | 专项复核 Flop 继续、Turn 主动下注、River raise | **是，先回放** | 差异样本量足够，但翻后状态无法完整重建。应从 pokerbot/AinP 原生事件制作同局面回放，按牌型、SPR、人数、to-call、是否前街进攻者细分后再改阈值。 |
| P1 | 校验并收窄 AinP 尺度 | **是，先校验 amount 语义** | 同主动动作时 AinP 中位 0.762 pot、BenAPI 0.50 pot，仅约一半相差不超过 0.25 pot。先确认 raise 增量/to-call 口径，再做每街、每 profile 的 shadow sizing。 |
| P1/P2 | 持久化对手模型并利用摊牌 | **是，分阶段** | AinP 当前 tracker 只在单进程内按动作累计；重启丢失，也不从 showdown/profit 更新 range。先持久化带样本数和衰减的 VPIP/PFR/3bet/cbet，再研究摊牌 range；必须隔离机器人互打数据并设置最小样本。 |
| P2 | 保存 rake/insurance 与净收益分解 | **是，分析用途** | 当前 end_hand 只记录 profit，BenAPI 回调字段更完整。补齐后可算 gross/net BB100 和费用归因，但不应直接改变单次动作。 |
| P1（条件） | AinP 专用 AOF Push/Fold EV | **AOF 要迁移则是** | BenAPI 有 66,820 条带 Push/Fold EV 的成功样本；AinP 普通 NLH 规则 + legal guard 不等价。应独立建模人数、位置、ante、stack、已 Push 人数和短牌开关。 |
| P2 | Hold'em API 错误原因与 adapter 前置校验 | **是，若继续使用 BenAPI** | 0.198% 决策失败和 1.032% 回调失败虽不高，但跨四天稳定，大量是可在本地发现的动作状态、max raise、player 0 问题。请求前校验能减少无效调用和 fallback。 |
| P1 | 建立受控收益实验与完整费用采集 | **是，调策略前先做** | 当前总体收益区间跨 0，日/实例波动远大于均值，rake 仅 99 手非零。按桌随机分配 AinP/BenAPI/profile，记录 rake、保险、对手类型，并用时间块/桌级 bootstrap 评价净 BB/100。 |
| P2 | 保留并定期运行本分析器 | **是** | schema 4 已支持递归去重、按日期/实例收益、置信区间、AOF 和多 profile 比较。建议固定命令并按 policy/BenAPI 版本保存趋势；新增日志后先跑 dry data-quality，再跑昂贵策略对比。 |
| 暂缓 | 新建 35/23 profile 模仿 shark | **暂不建议** | shark 实测约 35.9/23.5，但动作最接近的是 AinP 30/15；VPIP/PFR 相似不等于策略相同。只有“行为兼容”是产品目标且 A/B 盈利不下降时才新增。 |
| 暂缓 | 直接把 BenAPI 概率暴露成生产 API | **暂不建议** | AinP 当前规则没有校准过的完整概率分布。可以先加 debug/audit 字段，不能把单个确定动作伪装成概率。 |
| 不做 | 改成 BenAPI 式无状态全量快照 | **不建议** | 会丢失 AinP 已有的严格时序、幂等、延迟看牌、偏差检测和回放优势，也不能自动消除快照内部矛盾。 |
| 不做 | 增加 `anti_cheat_mode` 或读取隐藏牌 | **不建议** | 不符合 AinP 的公平信息边界；本批 BenAPI 日志也全部关闭该模式。 |
| 暂缓 | MTT/Spin/Squid 全字段补齐 | **当前不建议** | 本批 2,113,585 条 Hold'em 请求覆盖率均为 0。等真实玩法接入时，应按 ICM/奖励效用完整设计，不做空 DTO。 |

## 推荐实施顺序

1. 先修 pokerbot AOF callback 映射和非法动作 fallback，避免继续产生污染数据。
2. AinP 先做网络重试/整手 sync-broken 和 STR/forced-post 状态正确性，这些属于基础输入与可靠性。
3. 先建立同桌随机分流和完整 rake/insurance 采集，按桌/时间块输出净 BB/100 和置信区间。
4. 用原生 AinP 事件补采可完整恢复的 shadow 数据，再验证 Flop/Turn/River 差异。
5. 在离线 replay 中加入候选动作权重、尺度候选和对手模型持久化实验。
6. 最后才决定是否上线混合策略、尺度调整或新 profile；上线必须走现有 gray/canary 和受控 A/B。

本轮只优化了离线分析器和报告，没有修改 AinP 生产策略、协议或 pokerbot 生产代码。
