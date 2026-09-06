# AI 开发指南

## 项目边界

ainp 是独立 Go module，入口在 main.go。HTTP 只负责鉴权、绑定和返回；事件校验与编排放 internal/service；协议 DTO 放 internal/protocol；状态实现放 internal/store。禁止把策略堆进 Gin handler。

第 1 阶段接口骨架、第 2 阶段多玩法 equity、第 3 阶段第一版策略和第 4 阶段人格/基础玩家模型已完成。AI 修改代码前必须先读：README.md、本文件、protocol.md、equity-engine.md、strategy-rules.md、personality-model.md、player-model.md 和相关测试。修改 HTTP 协议时还必须阅读 api/openapi.yaml；协议冲突时以调用方实际生成类型和请求行为为准，并补契约测试。

## 开发顺序

1. 保持现有 HTTP 契约、牌型穷举测试和 equity 确定性测试。
2. 保持归一化 GameState、轮转和事件状态机的确定性回放。
3. 保持 Legal Action Guard 与推导金额的双层保护。
4. 保持已完成的事件状态、合法动作、equity/strategy HTTP 链路契约。
5. 将基础玩家快照升级为 Range 对手建模和离线回放评估。
6. 增加人格/规则版本配置、持久化和线上灰度观测。

每次只交付一个可验证的小目标。新算法必须提供接口、确定性测试向量、基准和降级路径。不要删除五张牌全量组合测试，也不要用提高默认采样数掩盖算法偏差。AI 生成的牌型、概率、金额换算和并发代码必须人工 review；不得用大规模重构混入功能变更。

## 兼容与安全

- 不改 /v1/event 字段、枚举、状态码和金额含义；扩展必须版本化。
- 不读取未发出的牌、真人底牌或 RNG 内部状态。
- 不提交 token、历史生产牌局原文或用户标识。
- 不把 Mock provider 标记为生产策略。
- 多实例上线前不能继续使用单机内存状态。

## 完成定义

代码需通过格式化、vet、race test、普通测试和构建；更新对应文档；记录未解决限制。对 pokerbot 的接入变更必须支持按配置回滚到第三方 URL，并建议按机器人/牌桌/流量逐级灰度。
