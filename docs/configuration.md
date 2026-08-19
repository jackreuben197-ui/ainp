# 配置说明

运行审计后台的开关、文件路径和刷新策略见 [runtime-dashboard.md](runtime-dashboard.md)。后台配置为可选扩展，默认值不会改变现有 `/v1` 协议。

配置文件使用严格 YAML；未知字段会导致启动失败，避免拼写错误被静默忽略。环境变量目前支持 AINP_MODE、AINP_SERVER_HOST、AINP_SERVER_PORT、AINP_AUTH_TOKEN 和 AINP_LOG_LEVEL。

## 模式与降级

- `mode: engine`：真实状态 + Equity + 策略；要求 engine/equity/strategy/infer_legal_actions 全部启用。
- `mode: mock`：只返回 mock 配置，用于协议诊断。
- `engine.fallback_to_mock`：真实计算超时或失败时是否使用 mock；失败和降级都会单独记录。
- `engine.advise_on`：允许触发决策的事件类型，默认 deal/action/flop/turn/river；即使事件在列表中，也只有轮到英雄才建议。

## 计算与资源

- `decision_timeout`：一次策略/Equity 计算上限，不包含可选思考等待。
- `max_concurrent`：计算并发槽，4CPU 推荐 3。
- `default_level`：第一版策略强度 1～5。
- `game_aliases`：外部玩法名映射到 NLH/PLO4/PLO5/PLO6/SHORT_DECK/SHORT_DECK_FIXED。默认将 NLHB/NLP 映射为 NLH、PLO 映射为 PLO4。
- `equity.*_samples`、`max_exact_outcomes`：各玩法采样与精确枚举边界。
- `preflop_lookup_enabled/auto_exact_enabled`：翻牌前查表和小搜索空间自动精确枚举开关；关闭后 Auto 使用 Monte Carlo。
- `cache_enabled/cache_capacity`：精确结果进程内缓存。
- `state.ttl/max_hands/prune_interval`：牌局状态留存和容量边界。

## 策略与拟人

- `strategy.enabled`：策略总开关；真实模式必须开启。
- `strategy.infer_legal_actions`：从事件推导合法动作；AiCon v1 必须开启。
- `strategy.min_raise_big_blinds`：最小完整下注/加注的盲注下限。
- `preflop_open_call_gap`：开池加注阈值与允许入池阈值的差，越小越紧。
- `preflop_reraise_equity`：面对加注时再加注的单挑起手牌强度基线。
- `preflop_extra_raise_penalty`：面对第二次及后续加注时提高再加注阈值。
- `preflop_multiway_penalty`：每增加一名对手对开池/再加注阈值的收紧幅度。
- `preflop_call_margin`：相对 Pot Odds 的基础安全边际。
- `personality.enabled`：人格阈值与下注尺度。
- `humanization_enabled`：有界慢打/边缘失误。
- `think_time_enabled`：生成确定性思考时间。
- `apply_think_time`：在返回 advice 前实际等待思考时间；关闭时仍可在策略日志观察建议时间。
- `use_ai_profile/default/profile_map`：兼容旧的 AiProfile→人格字符串映射；未知值使用 default 和 `engine.default_level`。
- `personality.profiles`：推荐的复合风格映射。每个 AiProfile 可同时指定 `personality`、1～5 的 `level`、`target_vpip`、`target_pfr` 和说明。目标必须满足 `0 <= PFR <= VPIP <= 1`。它在每次 `start_hand_extended` 独立解析，同一 AinP 实例可并行服务不同风格，机器人也可在下一手切换风格。
- `FPCH_100_50` 是特殊永不弃牌风格：`target_vpip: 1`、`preflop_raise_probability: 0.5`、`postflop_aggression_probability: 0.75`。`behavior_mode: aggressive_never_fold` 启用独立概率策略，`never_fold: true` 保证合法动作降级也只会 Call/Check/All-in，`audit_exempt: true` 将其策略行为从普通异常门禁移到独立统计块。所有概率均可在该 profile 下调整，无需修改接口或部署独立实例。
- 配置已覆盖 AiCon `fpch_profile.csv` 的 19 个名字（含 S1/S2 后缀）。S1/S2 在原说明中只区分翻牌后下注尺度，因此继承同一基础人格和等级；完整原尺度写在各项 `description` 中。
- `opponent_model.enabled/max_players/dedupe_window`：基础玩家统计及内存边界。

## 日志

`log.access`、`log.events`、`log.strategy` 分别控制每次 HTTP、事件结果和策略明细日志。生产排错与统计建议全部开启；如磁盘压力过大，应由日志采集器缩短本地留存，不建议关闭 access/events。

`strategy_decision` 中的 `ai_profile` 是调用方原值，`personality_id`、`strategy_level`、`target_vpip`、`target_pfr`、`behavior_mode`、`audit_exempt` 是本手牌实际解析结果，`profile_source` 表示来自 `profiles`、旧 `profile_map`、内置人格名或默认降级。验收时应按这些字段分组，不能只看总体 Fold 率；生产门禁默认不允许出现未配置风格的 `default` 降级。

修改风格范围后执行百万手验收：

```bash
go run ./cmd/profilesim \
  -config conf/config.yaml \
  -hands 1000000 \
  -tolerance 0.002 \
  -output reports/profile-sim-latest.json
```

`hands` 是每个已配置 profile 的样本量，而不是所有 profile 合计样本量。程序调用真实策略引擎，任一风格 VPIP/PFR 的绝对误差超过容差会以非零状态退出。

`FPCH_90_5` 还配置 `large_pot_threshold_bb` 和 `large_pot_min_equity`：保持 90/5 翻前统计；达到 20BB 大底池阈值后，低于 58% 估算胜率只 check/fold。收益基准可执行：

```bash
go run ./cmd/profitsim \
  -config conf/config.yaml \
  -profile FPCH_90_5 \
  -hands 1000000 \
  -equity-samples 16 \
  -rake 0.05 \
  -output reports/profit-sim-FPCH_90_5-1m.json
```

该工具模拟 100BB 单挑、固定松被动对手、完整公共牌、真实策略决策及 5% 抽水，报告总胜率、摊牌胜率、BB/100、95% 置信区间及大小底池收益。只有 BB/100 的 95% 置信区间下界为正、大底池总收益为正且小底池平均损失不超过 0.5BB 才标记 `passed=true`，否则非零退出。它是可重复的回归基准，不代表对任意真人、任意抽水结构都保证盈利。

## 第5阶段：回放与灰度

- `engine.policy_version`：本次部署的策略版本，写入策略日志和回放档案。
- `phase5.replay.enabled`：是否保存可重放的完整事件与响应；默认生产配置开启。
- `phase5.replay.directory/file_prefix`：回放 JSONL 的目录和文件名前缀；每次进程启动创建一个文件。
- `phase5.replay.flush_each_write`：每条事件后刷盘，生产建议开启，优先保证崩溃现场完整。
- `phase5.gray.enabled`：内部策略灰度总开关。
- `phase5.gray.mode`：`shadow` 只对采样手牌计算候选策略且仍返回基准策略；`canary` 对采样手牌真实返回候选策略。
- `phase5.gray.percentage/salt`：0～100 的稳定手牌分桶比例和盐。同一玩家、牌桌、手牌始终落入同一组，不会在一手牌中切换策略。
- `phase5.gray.candidate.*`：候选策略版本和可选参数覆盖；未填写的参数继承基准引擎。

首次上线应使用 `shadow`，积累足够回放与对比数据后再将少量流量切到 `canary`。回放文件包含英雄手牌、公共牌和合法亮牌，属于生产敏感数据，需要限制目录权限与留存时间。
