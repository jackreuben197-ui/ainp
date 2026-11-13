#!/usr/bin/env python3
"""Analyze archived AINP nohup logs without requiring replay JSONL files.

The generated analysis keeps the same section and column layout as the replay
profit reports.  AINP's current nohup format records one ``hand_result`` per
robot, but not the complete table player list or every player's settlement.
Human-side cells therefore remain explicit ``N/A*`` values instead of being
silently removed or incorrectly inferred from robot profit.
"""

from __future__ import annotations

import argparse
import json
import math
import re
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Iterable


BEIJING = timezone(timedelta(hours=8))
DEFAULT_BBS = (0.2, 0.6, 2.0)

TIME_RE = re.compile(r'"time":"([^"]+)"')
TABLE_RE = re.compile(r'"table_id":"([^"]+)"')
HAND_RE = re.compile(r'"hand_id":"([^"]+)"')
PLAYER_RE = re.compile(r'"player_id":"([^"]+)"')
PROFILE_RE = re.compile(r'"ai_profile":"([^"]*)"')
PROFIT_RE = re.compile(r'"profit":(-?(?:\d+(?:\.\d*)?|\.\d+))')
TABLE_PROFIT_RE = re.compile(r'"table_net_profit":(-?(?:\d+(?:\.\d*)?|\.\d+))')
TABLE_PROFIT_BB_RE = re.compile(
    r'"profit_control_profit_bb":(-?(?:\d+(?:\.\d*)?|\.\d+))'
)
REACHED_RE = re.compile(r'"reached_street":"([^"]*)"')
BOARD_COUNT_RE = re.compile(r'"board_card_count":(\d+)')
SHOWDOWN_RE = re.compile(r'"showdown_players":(\d+)')


def parse_time(value: str) -> datetime:
    value = re.sub(
        r"\.(\d+)(Z|[+-]\d\d:\d\d)$",
        lambda match: "." + (match.group(1) + "000000")[:6] + match.group(2),
        value,
    )
    return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(BEIJING)


def parse_cutoff(value: str) -> datetime:
    parsed = datetime.fromisoformat(value.replace(" ", "T"))
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=BEIJING)
    return parsed.astimezone(BEIJING)


def match(pattern: re.Pattern[str], line: str, default: str = "") -> str:
    found = pattern.search(line)
    return found.group(1) if found else default


def nearest_bb(value: float, expected: tuple[float, ...]) -> float | None:
    if not math.isfinite(value) or value <= 0:
        return None
    nearest = min(expected, key=lambda candidate: abs(candidate - value))
    tolerance = max(1e-6, nearest * 1e-6)
    return nearest if abs(nearest - value) <= tolerance else None


def discover_files(root: Path) -> list[Path]:
    return sorted(path for path in root.rglob("nohup.out*") if path.is_file())


@dataclass(frozen=True)
class HandResult:
    time: datetime
    table_id: str
    hand_id: str
    player_id: str
    profile: str
    profit: float
    reached_street: str
    board_card_count: int
    showdown_players: int


@dataclass
class Metric:
    hands: set[tuple[str, str]] = field(default_factory=set)
    assignments: int = 0
    wins: int = 0
    losses: int = 0
    ties: int = 0
    profit: float = 0.0
    profit_bb: float = 0.0

    def add(self, key: tuple[str, str], profit: float, bb: float) -> None:
        self.hands.add(key)
        self.assignments += 1
        self.profit += profit
        self.profit_bb += profit / bb
        if profit > 0:
            self.wins += 1
        elif profit < 0:
            self.losses += 1
        else:
            self.ties += 1


def iter_lines(files: Iterable[Path]) -> Iterable[str]:
    for path in files:
        with path.open(errors="replace") as source:
            yield from source


def first_pass(
    files: list[Path], expected_bbs: tuple[float, ...]
) -> tuple[
    dict[str, datetime],
    dict[tuple[str, str, str], HandResult],
    dict[str, Counter[float]],
    int,
]:
    room_created: dict[str, datetime] = {}
    results: dict[tuple[str, str, str], HandResult] = {}
    bb_votes: dict[str, Counter[float]] = defaultdict(Counter)
    conflicts = 0

    for line in iter_lines(files):
        if '"msg":"decision_event"' in line and '"cmd":"start_hand_extended"' in line:
            if match(HAND_RE, line) != "1":
                continue
            table_id = match(TABLE_RE, line)
            time_text = match(TIME_RE, line)
            if not table_id or not time_text:
                continue
            observed = parse_time(time_text)
            room_created[table_id] = min(room_created.get(table_id, observed), observed)
            continue

        if '"msg":"strategy_decision"' in line:
            table_id = match(TABLE_RE, line)
            net_text = match(TABLE_PROFIT_RE, line)
            net_bb_text = match(TABLE_PROFIT_BB_RE, line)
            if not table_id or not net_text or not net_bb_text:
                continue
            net_profit, net_profit_bb = float(net_text), float(net_bb_text)
            if net_profit == 0 or net_profit_bb == 0:
                continue
            bb = nearest_bb(abs(net_profit / net_profit_bb), expected_bbs)
            if bb is not None:
                bb_votes[table_id][bb] += 1
            continue

        if '"msg":"hand_result"' not in line:
            continue
        table_id = match(TABLE_RE, line)
        hand_id = match(HAND_RE, line)
        player_id = match(PLAYER_RE, line)
        time_text = match(TIME_RE, line)
        profit_text = match(PROFIT_RE, line)
        if not all((table_id, hand_id, player_id, time_text, profit_text)):
            continue
        record = HandResult(
            time=parse_time(time_text),
            table_id=table_id,
            hand_id=hand_id,
            player_id=player_id,
            profile=match(PROFILE_RE, line),
            profit=float(profit_text),
            reached_street=match(REACHED_RE, line),
            board_card_count=int(match(BOARD_COUNT_RE, line, "0")),
            showdown_players=int(match(SHOWDOWN_RE, line, "0")),
        )
        key = (table_id, hand_id, player_id)
        previous = results.get(key)
        if previous is not None and (
            previous.profit != record.profit or previous.profile != record.profile
        ):
            conflicts += 1
        if previous is None or record.time >= previous.time:
            results[key] = record

    return room_created, results, bb_votes, conflicts


def choose_table_bbs(
    votes: dict[str, Counter[float]], eligible_tables: set[str]
) -> dict[str, float]:
    chosen: dict[str, float] = {}
    for table_id in eligible_tables:
        if votes.get(table_id):
            chosen[table_id] = sorted(
                votes[table_id].items(), key=lambda item: (-item[1], item[0])
            )[0][0]
    return chosen


def collect_top_actions(
    files: list[Path], top_keys: set[tuple[str, str]]
) -> dict[tuple[str, str], list[dict[str, object]]]:
    actions: dict[tuple[str, str], list[dict[str, object]]] = defaultdict(list)
    for line in iter_lines(files):
        if '"msg":"strategy_decision"' not in line:
            continue
        table_id = match(TABLE_RE, line)
        hand_id = match(HAND_RE, line)
        key = (table_id, hand_id)
        if key not in top_keys:
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        actions[key].append(
            {
                "time": parse_time(str(row.get("time", ""))),
                "player_id": str(row.get("player_id", "")),
                "profile": str(row.get("ai_profile", "")),
                "street": str(row.get("street", "")),
                "hand_class": str(row.get("hero_hand_class", "")),
                "action": str(row.get("action", "")),
                "amount": float(row.get("amount", 0) or 0),
                "pot": float(row.get("pot", 0) or 0),
                "to_call": float(row.get("to_call", 0) or 0),
                "effective_stack": float(row.get("effective_stack", 0) or 0),
                "rule_id": str(row.get("rule_id", "")),
            }
        )
    for rows in actions.values():
        rows.sort(key=lambda row: row["time"])
    return actions


def fmt(value: float, digits: int = 2) -> str:
    return f"{value:.{digits}f}"


def analyze(args: argparse.Namespace) -> dict[str, object]:
    root = args.input_root.expanduser().resolve()
    output = args.output_dir.expanduser().resolve()
    cutoff = parse_cutoff(args.cutoff)
    expected_bbs = tuple(args.expected_bb)
    files = discover_files(root)
    if not files:
        raise SystemExit(f"no nohup.out* files found under {root}")

    room_created, results, bb_votes, conflicts = first_pass(files, expected_bbs)
    eligible_tables = {
        table_id for table_id, created in room_created.items() if created > cutoff
    }
    excluded_before = set(room_created) - eligible_tables
    result_tables = {record.table_id for record in results.values()}
    missing_first = result_tables - set(room_created)
    table_bbs = choose_table_bbs(bb_votes, eligible_tables)
    missing_bb_tables = eligible_tables - set(table_bbs)

    per_hand: dict[tuple[str, str], list[HandResult]] = defaultdict(list)
    for record in results.values():
        if record.table_id in eligible_tables:
            per_hand[(record.table_id, record.hand_id)].append(record)
    table_robot_ids: dict[str, set[str]] = defaultdict(set)
    for records in per_hand.values():
        for record in records:
            table_robot_ids[record.table_id].add(record.player_id)

    bb_metrics: dict[float, Metric] = defaultdict(Metric)
    profile_metrics: dict[tuple[float, str], Metric] = defaultdict(Metric)
    player_metrics: dict[tuple[float, str], Metric] = defaultdict(Metric)
    table_metrics: dict[str, Metric] = defaultdict(Metric)
    profile_audit = Counter()
    anomaly_rows: list[tuple[tuple[str, str], list[HandResult]]] = []
    hand_totals: list[dict[str, object]] = []

    for key, records in per_hand.items():
        table_id, _ = key
        bb = table_bbs.get(table_id)
        profiles = [record.profile for record in records]
        aggressive_count = profiles.count("FPCH_100_50")
        if aggressive_count == 1:
            category = "exactly_one_aggressive"
        elif aggressive_count > 1:
            category = "multiple_aggressive"
        elif profiles and all(profile == "FPCH_0_0" for profile in profiles):
            category = "all_check_or_fold"
        elif "FPCH_0_0" in profiles:
            category = "mixed_check_or_fold_and_normal"
        else:
            category = "no_special_profile"
        profile_audit[category] += 1
        if category not in ("exactly_one_aggressive", "all_check_or_fold"):
            anomaly_rows.append((key, records))

        robot_profit = sum(record.profit for record in records)
        if bb is None:
            continue
        for record in records:
            bb_metrics[bb].add(key, record.profit, bb)
            profile_metrics[(bb, record.profile)].add(key, record.profit, bb)
            player_metrics[(bb, record.player_id)].add(key, record.profit, bb)
            table_metrics[table_id].add(key, record.profit, bb)
        hand_totals.append(
            {
                "key": key,
                "bb": bb,
                "profit": robot_profit,
                "profit_bb": robot_profit / bb,
                "records": records,
            }
        )

    top20 = sorted(
        hand_totals,
        key=lambda row: (row["profit_bb"], row["profit"], row["key"]),
    )[: args.top]
    top_keys = {row["key"] for row in top20}
    top_actions = collect_top_actions(files, top_keys)

    output.mkdir(parents=True, exist_ok=True)
    prefix = args.report_prefix or f"profit-ainp-log-post-{cutoff:%Y%m%d-%H%M%S}"
    analysis_path = output / f"{prefix}-analysis.md"
    players_path = output / f"{prefix}-players.md"
    top_path = output / f"{prefix}-top{args.top}-robot-traces.md"
    anomalies_path = output / f"{prefix}-profile-anomalies.md"

    report: list[str] = [
        f"# ainp_log：{cutoff.isoformat()} 后新房盈利分析",
        "",
        "## 统计口径",
        "",
        f"- 输入根目录：`{root}`；递归读取 {len(files)} 个 `nohup.out*`。",
        f"- 仅纳入明确观察到第1手且第1手时间严格晚于 `{cutoff.isoformat()}` 的房间。",
        "- 同一机器人同一手重复记录按时间较新的记录去重。",
        "- BB 从同桌日志的 `abs(table_net_profit / profit_control_profit_bb)` 多数值推导，并限制为配置的BB档位。",
        "- 所有实例日志已按 `table_id + hand_id` 混合；`hand_result.player_id` 的并集用于识别全部已记录机器人。",
        "- 当前输入没有 replay，且 nohup 的 `hand_result` 只记录当前机器人，不含全桌 `end_hand.players`。因此真人身份、真人参局次数和真人结算无法严谨恢复；对应单元格保留为 `N/A*`，不以机器人净输赢反推。",
        "",
        "## 数据范围",
        "",
        f"- 有第1手的房间 {len(room_created)} 个：纳入 {len(eligible_tables)} 个，截止前排除 {len(excluded_before)} 个。",
        f"- 另有 {len(missing_first)} 个出现结算但没有第1手记录的房间，排除。",
        f"- 纳入房间中 {len(missing_bb_tables)} 个无法可靠推导BB，其结算保留在画像审计中但不进入分BB盈利。",
        f"- 纳入 {len(per_hand)} 个物理手、{sum(len(rows) for rows in per_hand.values())} 个机器人结算、{len({record.player_id for rows in per_hand.values() for record in rows})} 个已记录机器人ID；冲突重复记录 {conflicts} 条。",
        f"- 最早纳入房间：{min(room_created[t] for t in eligible_tables).isoformat() if eligible_tables else '无'}；最晚：{max(room_created[t] for t in eligible_tables).isoformat() if eligible_tables else '无'}。",
        "",
        "## 分 BB 总体盈利",
        "",
        "| BB | 房间 | 已结算手数 | 机器人结算次数 | 机器人净盈利BB | 机器人BB/100 | 真人参局次数 | 真人胜/负/平 | 真人净盈利BB | 真人BB/100 | 对真人物理手数 | 全机器人手数 |",
        "|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for bb in sorted(bb_metrics):
        metric = bb_metrics[bb]
        bb100 = metric.profit_bb / len(metric.hands) * 100 if metric.hands else 0
        room_count = sum(1 for table in eligible_tables if table_bbs.get(table) == bb)
        report.append(
            f"| {bb:g} | {room_count} | {len(metric.hands)} | {metric.assignments} | "
            f"{fmt(metric.profit_bb,4)} | {fmt(bb100,2)} | N/A* | N/A* | N/A* | N/A* | N/A* | N/A* |"
        )

    report.extend(
        [
            "",
            "**注：** `N/A` 不是 0。当前 nohup 缺少完整玩家/结算集合；混合所有实例只能确认哪些 ID 是机器人，不能凭空得到没有出现在日志中的真人 ID、人数和输赢。",
        ]
    )

    report.extend(
        [
            "",
            "## 分 BB、分风格",
            "",
            "| BB | ai_profile | 手数 | 结算次数 | 胜/负/平 | 净盈利筹码 | 净盈利BB | 每100次结算BB |",
            "|---:|---|---:|---:|---:|---:|---:|---:|",
        ]
    )
    for (bb, profile), metric in sorted(profile_metrics.items()):
        rate = metric.profit_bb / metric.assignments * 100 if metric.assignments else 0
        report.append(
            f"| {bb:g} | {profile or '空'} | {len(metric.hands)} | {metric.assignments} | "
            f"{metric.wins}/{metric.losses}/{metric.ties} | {fmt(metric.profit)} | "
            f"{fmt(metric.profit_bb,4)} | {fmt(rate,2)} |"
        )

    passed = profile_audit["exactly_one_aggressive"] + profile_audit["all_check_or_fold"]
    audited = sum(profile_audit.values())
    report.extend(
        [
            "",
            "## ai_profile 强约束核验",
            "",
            f"- 观察 {audited} 手，通过 {passed} 手，通过率 {passed / audited * 100 if audited else 0:.4f}%。",
            f"- 恰好一个 `FPCH_100_50`：{profile_audit['exactly_one_aggressive']} 手。",
            f"- 全部已观察机器人均为 `FPCH_0_0`：{profile_audit['all_check_or_fold']} 手。",
            f"- 多个 `FPCH_100_50`：{profile_audit['multiple_aggressive']} 手。",
            f"- 无100_50且0_0混普通风格：{profile_audit['mixed_check_or_fold_and_normal']} 手。",
            f"- 无100_50且没有0_0：{profile_audit['no_special_profile']} 手。",
            "- 因缺少完整玩家列表，无法证明每手机器人画像覆盖完整；该结果只能审计日志中实际出现的机器人。",
            "",
            "## 异常画像牌局对真人汇总",
            "",
            "当前 nohup 没有完整玩家集合，不能筛出“对真人”的异常画像子集，也不能计算真人结算。保留上一版报告结构如下。",
            "",
            "| BB | 对真人物理手数 | 机器人整手胜/负/平 | 机器人净盈利BB | 机器人BB/100 | 真人参局次数 | 真人净盈利BB | 真人BB/100 |",
            "|---:|---:|---:|---:|---:|---:|---:|---:|",
        ]
    )
    for bb in sorted(bb_metrics):
        report.append(f"| {bb:g} | N/A* | N/A* | N/A* | N/A* | N/A* | N/A* | N/A* |")

    report.extend(
        [
            "",
            "## 存在 FPCH_100_50 但输给真人",
            "",
            "当前 nohup 能确认 `FPCH_100_50` 机器人的自身输赢，但没有真人结算，不能判断其输家是否为真人或其他机器人。",
            "",
            "| BB | 含100_50的对真人手数 | 100_50本人亏损手数/占比 | 100_50本人净亏BB | 对应真人参局次数 | 对应真人净盈利BB | 机器人整体亏损手数/占比 | 机器人整体净亏BB |",
            "|---:|---:|---:|---:|---:|---:|---:|---:|",
        ]
    )
    for bb in sorted(bb_metrics):
        report.append(f"| {bb:g} | N/A* | N/A* | N/A* | N/A* | N/A* | N/A* | N/A* |")

    report.extend(
        [
            "",
            "## 分 BB、分牌桌盈利",
            "",
            "| BB | 牌桌 | 首手时间（北京时间） | 手数 | 机器人/真人人数 | 机器人净盈利BB | 机器人BB/100 | 真人参局次数 | 真人净盈利BB | 真人BB/100 |",
            "|---:|---|---|---:|---:|---:|---:|---:|---:|---:|",
        ]
    )
    for table_id in sorted(table_metrics, key=lambda table: (table_bbs[table], table)):
        metric = table_metrics[table_id]
        bb = table_bbs[table_id]
        rate = metric.profit_bb / len(metric.hands) * 100 if metric.hands else 0
        report.append(
            f"| {bb:g} | {table_id} | {room_created[table_id].isoformat()} | "
            f"{len(metric.hands)} | {len(table_robot_ids[table_id])}/N/A* | {fmt(metric.profit_bb,4)} | "
            f"{fmt(rate,2)} | N/A* | N/A* | N/A* |"
        )
    analysis_path.write_text("\n".join(report) + "\n")

    player_report = [
        f"# AINP nohup：{cutoff.isoformat()} 后新房机器人明细",
        "",
    ]
    for bb in sorted(bb_metrics):
        player_report.extend(
            [
                f"## BB={bb:g}",
                "",
                "| 机器人ID | 物理手数 | 结算次数 | 胜/负/平 | 净盈利BB | 每100次结算BB |",
                "|---|---:|---:|---:|---:|---:|",
            ]
        )
        rows = sorted(
            ((player, metric) for (row_bb, player), metric in player_metrics.items() if row_bb == bb),
            key=lambda item: (-item[1].profit_bb, item[0]),
        )
        for player, metric in rows:
            rate = metric.profit_bb / metric.assignments * 100 if metric.assignments else 0
            player_report.append(
                f"| {player} | {len(metric.hands)} | {metric.assignments} | "
                f"{metric.wins}/{metric.losses}/{metric.ties} | {fmt(metric.profit_bb,4)} | {fmt(rate,2)} |"
            )
        player_report.append("")
    players_path.write_text("\n".join(player_report) + "\n")

    anomaly_report = [
        f"# AINP nohup：{cutoff.isoformat()} 后新房画像异常",
        "",
        "仅基于日志中实际出现的机器人；没有完整玩家列表，不能判断画像记录是否覆盖全部机器人。",
        "",
        "| 牌桌/手数 | 已观察机器人画像 | 机器人合计输赢 |",
        "|---|---|---:|",
    ]
    for key, records in sorted(anomaly_rows):
        detail = "; ".join(
            f"{record.player_id}:{record.profile or '空'}" for record in sorted(records, key=lambda row: row.player_id)
        )
        anomaly_report.append(
            f"| {key[0]} / {key[1]} | {detail} | {fmt(sum(record.profit for record in records))} |"
        )
    anomalies_path.write_text("\n".join(anomaly_report) + "\n")

    top_report = [
        f"# AINP nohup：{cutoff.isoformat()} 后新房机器人亏损最大{args.top}手",
        "",
        "这是机器人侧简化牌谱。源日志没有完整玩家列表、真人动作、实际底牌或公共牌；动作是AINP为各机器人给出的决策记录。",
        "",
        "## 排名",
        "",
        "| 排名 | BB | 牌桌/手数 | 机器人合计筹码 | 机器人合计BB |",
        "|---:|---:|---|---:|---:|",
    ]
    for index, row in enumerate(top20, 1):
        top_report.append(
            f"| {index} | {row['bb']:g} | {row['key'][0]} / {row['key'][1]} | "
            f"{fmt(row['profit'])} | {fmt(row['profit_bb'],4)} |"
        )
    for index, row in enumerate(top20, 1):
        key = row["key"]
        top_report.extend(
            [
                "",
                f"## {index}. 牌桌 {key[0]}，第 {key[1]} 手",
                "",
                f"- BB：{row['bb']:g}",
                f"- 房间首手时间：{room_created[key[0]].isoformat()}",
                f"- 机器人合计：**{fmt(row['profit'])} 筹码（{fmt(row['profit_bb'],4)} BB）**",
                "",
                "### 机器人结算",
                "",
            ]
        )
        for record in sorted(row["records"], key=lambda item: item.player_id):
            top_report.append(
                f"- {record.player_id}（{record.profile or '空'}）：{fmt(record.profit)}，"
                f"到达{record.reached_street or '未知'}，牌面张数{record.board_card_count}，"
                f"摊牌人数{record.showdown_players}。"
            )
        top_report.extend(["", "### AINP动作", ""])
        rows = top_actions.get(key, [])
        if not rows:
            top_report.append("- 未找到策略动作记录。")
        for action in rows:
            top_report.append(
                f"- {action['time'].isoformat()} {action['player_id']} "
                f"{action['street']} `{action['hand_class'] or '未知牌类'}`："
                f"{action['action']} {action['amount']:g}；底池{action['pot']:g}，"
                f"跟注额{action['to_call']:g}，有效筹码{action['effective_stack']:g}，"
                f"规则`{action['rule_id']}`。"
            )
    top_path.write_text("\n".join(top_report) + "\n")

    return {
        "files": len(files),
        "rooms_with_first_hand": len(room_created),
        "eligible_rooms": len(eligible_tables),
        "excluded_before_cutoff": len(excluded_before),
        "excluded_missing_first": len(missing_first),
        "missing_bb_rooms": len(missing_bb_tables),
        "physical_hands": len(per_hand),
        "robot_settlements": sum(len(rows) for rows in per_hand.values()),
        "profile_audit": dict(profile_audit),
        "outputs": [str(analysis_path), str(players_path), str(top_path), str(anomalies_path)],
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--input-root",
        type=Path,
        default=Path(__file__).resolve().parent.parent / "build" / "ainp_log",
    )
    parser.add_argument("--cutoff", default="2026-09-15 02:00:00")
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path(__file__).resolve().parent.parent / "reports",
    )
    parser.add_argument("--report-prefix")
    parser.add_argument("--top", type=int, default=20)
    parser.add_argument(
        "--expected-bb",
        type=float,
        nargs="+",
        default=list(DEFAULT_BBS),
        help="allowed BB values used to validate inferred table BB",
    )
    args = parser.parse_args()
    if args.top < 1 or any(bb <= 0 for bb in args.expected_bb):
        parser.error("--top and every --expected-bb value must be positive")
    return args


if __name__ == "__main__":
    print(json.dumps(analyze(parse_args()), ensure_ascii=False, indent=2))
