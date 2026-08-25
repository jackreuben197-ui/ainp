# ainp

ainp 是德州扑克机器人决策引擎服务。当前已完成第 1 阶段接口骨架、第 2 阶段多玩法牌力与胜率引擎、第 3 阶段第一版规则策略，以及第 4 阶段人格与基础玩家模型。HTTP 协议与 pokerbot/engine/aicon 使用的 AiCon API 兼容。

当前能力：

- POST /v1/check 与 POST /v1/event
- Bearer Token 和 access_token query 鉴权
- 牌局事件顺序、幂等和基础字段校验
- 与 AiCon 相同的成功、错误及 208 返回格式
- 每次 API 调用的访问日志，以及可关联重试的结构化事件/策略决策日志
- 健康检查 GET /healthz
- NLH、两种短牌规则、PLO4/PLO5/PLO6 的最佳牌型计算
- 精确枚举、Monte Carlo、多人平分 equity 和上下文取消
- 169 类翻牌前单挑查表与有界精确结果缓存
- 位置化翻牌前规则、Pot Odds/Call EV/SPR、价值下注、听牌、C-bet 和合法动作保护
- 6 种稳定人格、拟人思考时间、有界失误/慢打，以及 VPIP/PFR/3-bet 等基础玩家画像
- AiCon 事件流牌局状态重建、轮次推导、真实策略 HTTP 建议和建议偏差回传

默认 `mode: engine` 已连接完整 equity/strategy/personality/opponent 链路；`mode: mock` 只用于接口诊断，真实引擎计算失败时也可配置是否回退 Mock。ainp 不需要修改 pokerbot 即可通过现有 AiCon 事件接口返回动作。配置说明见 docs/configuration.md，牌力与策略限制分别见 docs/equity-engine.md 和 docs/strategy-rules.md。

## 本地运行

    go run . -config conf/config.yaml

将 pokerbot 配置改为：

    aicon:
      url: http://127.0.0.1:8090
      token: local-development-token

验证：

    go test ./...
    go build ./...
    curl -i -X POST -H 'Authorization: Bearer local-development-token' http://127.0.0.1:8090/v1/check

完整协议见 api/openapi.yaml，开发约束与后续路线见 docs/ai-development-guide.md。

## 打包(linux)

    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/ainp .
