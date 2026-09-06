# 第5阶段：回放、对比和灰度

## 1. 日志自动验收

`cmd/logaudit` 读取 slog JSONL，并按 `conf/audit.yaml` 检查样本量、翻牌前弃牌/主动率、HTTP 错误、引擎失败、策略计算 P95、HTTP P95、高胜率弃牌、灰度候选错误，以及“规则准备继续但合法动作保护最终 Fold”的意图冲突。报告同时汇总灰度路由量和 shadow 动作一致率。

```bash
go run ./cmd/logaudit \
  -input build/nohup.out \
  -expect conf/audit.yaml \
  -output reports/audit-latest.json
```

只验证协议、事件序列和牌局状态重建时，使用快速状态回放，避免重新执行每一次胜率计算和策略决策：

```bash
go run ./cmd/replay \
  -state-only \
  -input data/replay/events-latest.jsonl \
  -config conf/config.yaml \
  -output reports/replay-state-latest.json
```

状态回放会重建旧版本拒绝事件之后的请求序号，并在报告中分别输出 `resolved_rejections`（修复版已接受的旧拒绝）和 `new_rejections`（修复版新增拒绝）。`new_rejections` 应为 0。需要评估策略动作变化时才省略 `-state-only`，执行完整策略对比。

如果下载的是仍在写入的 replay，最后一条 JSONL 可能只复制了一部分。回放会跳过这一条并报告 `truncated_tail_records: 1`；中间任意坏行仍立即失败。正式归档最好先轮转文件再下载，以获得完整尾记录。

通过时退出码为 0；指标不合格时退出码为 2，适合接入发布脚本或 CI。JSON 报告保留聚合指标、失败原因和最多 20 个异常决策样例。阈值是运行门禁，不是扑克盈利保证；新玩法或样本结构变化时应基于分层数据单独调整。

## 2. 生产事件录制

开启 `phase5.replay.enabled` 后，每次进程启动在 `data/replay` 生成一个 JSONL。每条记录含 schema 版本、原始 AiCon 事件、当时响应、错误结果、追踪 ID、provider 和策略版本。它保留了重建状态所需的全部有序事件，因此普通脱敏日志之外仍能复现具体决策。

启动日志 `replay_recorder_started.path` 给出准确文件名。生产默认逐条 flush；需要通过系统级轮转/归档管理留存，且不应把回放档案提交到 Git。

## 3. 离线回放与策略对比

```bash
go run ./cmd/replay \
  -config conf/config.yaml \
  -input data/replay/events-20260801T230000.jsonl \
  -output reports/replay-phase5-candidate.json
```

回放工具关闭真实思考等待，按原始顺序重建牌局，并用当前配置的引擎重新决策。报告包括：

- 可比较建议数和完全一致率；
- action_changed、value_changed、advice_added、advice_removed；
- `fold->call` 等动作迁移矩阵；
- 状态结果不一致数及每项变化的 decision_id/hand_id/seq_num。
- end_hand 中各机器人净输赢、胜/负/走平手数和按机器人汇总的 profit。
- 候选版本 applied/rejected/duplicate 结果、按错误码汇总及最多 100 条具体错误上下文，便于确认旧错误是否已被修复且没有引入新拒绝。

Monte Carlo 和拟人随机种子来自稳定 decision_id，同一档案与配置可重复得到相同结果。

## 4. Shadow 与 Canary

灰度按 `salt|player_id|table_id|hand_id` 做稳定哈希分桶。

- `shadow`：基准策略照常返回；采样手牌额外计算候选策略并写 `gray_decision`，适合先观察动作迁移和性能开销。
- `canary`：采样手牌直接返回候选策略，其余返回基准策略。只有在 shadow 和离线回放通过门禁后使用。

推荐顺序：离线回放 → 10% shadow → 50% shadow → 5% canary → 20% canary → 全量。每一步按玩法、人格、位置、街道分层检查，而不是只看全局 Fold 率。

## 5. 当前日志基线（2026-08-01）

本次 2,193 个 HTTP 请求、255 次策略决策中，翻牌前 137 次：Fold 61.31%，Raise 10.22%。策略计算 P95 约 40ms，包含拟人等待的 HTTP P95 约 1.40s，均符合当前门禁。20 个 400 全部来自同一手牌 438，在服务启动后收到 seq 10～13 而没有 seq 1，属于中途重启后的 Broken hand 集中事件。

门禁仍判定失败：有 13 次规则意图与最终 Fold 冲突，其中一次 77 的单挑起手牌 Equity 66.26%。根因是短筹码/跟注等于全栈时接口只提供 allin，旧合法动作降级链把 Call/Raise 退化为 Fold。本阶段已将这两类继续意图优先转换为 allin。必须用新二进制重新采样后再确认门禁转绿。
