# 对接协议

协议事实源为 api/openapi.yaml，它来自 pokerbot/engine/aicon/openapi.yaml。兼容端点为：

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | /v1/check | 鉴权及可用性检查 |
| POST | /v1/event | 提交单个牌局事件 |
| GET | /healthz | 不鉴权的实例健康检查（ainp 扩展） |

鉴权优先读取 Authorization Bearer token，缺失时读取 query 参数 access_token。事件键由 player_id + room_id + table_id + hand_id 构成；seq_num 从 1 开始且严格递增。

成功返回 HTTP 200，例如 {"seq_num":2,"advise":{"type":"raise","value":6}}。没有轮到机器人时只返回 seq_num。完全相同的最后一个事件重试返回 HTTP 208、空 body；序号错误返回 HTTP 400 及 AiCon 兼容错误体。401/403 按原协议保持空 body。

start_hand_extended、deal_cards、action、show_cards、flop、turn、river 和 end_hand 会更新规范化状态。返回过 advice 后，下一次英雄 action 会与建议比较；类型或金额不一致时在兼容的 deviation 字段中返回差异。tour_bonus/tour_exit 保持协议接收但不参与牌局策略。

`deal_cards` 不要求紧跟在 `start_hand_extended` 之后。延迟看牌房间可以先提交其他玩家动作，等轮到机器人时再以当前严格递增的 `seq_num` 提交手牌；只要该事件使机器人成为当前行动者，AinP 会在同一个响应中返回真实策略建议。部署审计会同时识别 `seq_num > 2` 和“开手后超过阈值才发牌”两类延迟发牌，并检查是否拒绝、是否漏回 advice 及处理延迟。

`start_hand_extended.payload.ai_profile` 由 pokerbot 的 `engineStyle` 每手传入。AinP 在该手牌 State 中独立保存它，并通过 `engine.personality.profiles` 解析实际人格和1～5等级；因此同一实例可同时服务不同机器人风格，也允许同一机器人从下一手开始切换，不需要按等级部署多个实例。

当机器人调度系统已经负责行动延迟时，配置 `engine.personality.apply_think_time: false`。AinP 仍可生成人格化思考时间用于日志和统计，但不会阻塞 HTTP 响应。
# Action amount compatibility

`action.payload.value_mode` is optional:

- omitted or `increment`: `value` is the chips added by this action (legacy AiCon behavior);
- `street_total`: `value` is the player's total contribution on the current betting street. AinP subtracts the contribution already tracked for that player before validation and state updates.

Pokerbot's native `ainp` engine sends `increment`. AinP also recognizes logs
produced by the older adapter that labelled an increment as `street_total` when
`stack_after` makes that mismatch unambiguous. Existing integrations do not
need to change, and unsupported values are rejected as game-logic errors.

The native adapter additionally sends two optional server-authoritative fields:

- `action.payload.stack_after`: the acting player's remaining stack after the action. AinP uses it to reconcile the final stack and forced contributions omitted from the start event; an explicit correctly labelled `value` remains the action amount.
- `payload.next_player_id`: the server-selected next actor on `deal_cards`, `action`, `flop`, `turn`, and `river`. An empty string means that the betting round currently has no next actor. When present, this takes precedence over locally inferred seat order.

Both fields are backward-compatible. Clients that omit them continue to use
`value_mode` normalization and AinP's inferred action order.

The native adapter also sends optional `legal_actions` on the event immediately
before the hero must act (`deal_cards`, `action`, `flop`, `turn`, or `river`).
Each entry contains `type`, `min`, and `max` in table chip units; `min/max` are
the amount added by the action, matching pokerbot's `ActionLimit`. AinP constrains
the strategy result to these server-provided actions and returns the amount as
`value_mode: increment`. When the field is absent, the original inferred
legal-action behavior remains in effect.
