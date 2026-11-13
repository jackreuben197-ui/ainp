#!/usr/bin/env python3
"""Analyze AINP replay shards and produce the established profit report set."""

from __future__ import annotations

import argparse
import json
import subprocess
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Iterable


BEIJING = timezone(timedelta(hours=8))


def parse_time(value: str) -> datetime:
    # Go emits up to nanoseconds; datetime accepts microseconds.
    if "." in value:
        head, tail = value.split(".", 1)
        zone_pos = max(tail.find("+"), tail.find("-"), tail.find("Z"))
        if zone_pos >= 0:
            fraction, zone = tail[:zone_pos], tail[zone_pos:]
            value = f"{head}.{(fraction + '000000')[:6]}{zone}"
    return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(BEIJING)


def parse_cutoff(value: str) -> datetime:
    parsed = datetime.fromisoformat(value.replace(" ", "T"))
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=BEIJING)
    return parsed.astimezone(BEIJING)


def discover(root: Path) -> list[Path]:
    return sorted(path for path in root.rglob("data/replay/*.jsonl") if path.is_file())


def scan_nohup_robot_ids(root: Path) -> set[str]:
    """Build the global robot universe from every AINP instance's hero logs."""
    pattern = r'"msg":"(?:hand_result|strategy_decision)".*?"player_id":"([^"]+)"'
    completed = subprocess.run(
        [
            "rg", "-o", "--no-filename", "--replace", "$1", "-g", "nohup.out*",
            pattern, str(root),
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    if completed.returncode not in (0, 1):
        raise RuntimeError(completed.stderr.strip() or "failed to scan robot IDs with rg")
    return {line.strip() for line in completed.stdout.splitlines() if line.strip()}


def iter_records(files: Iterable[Path]) -> Iterable[dict[str, Any]]:
    for path in files:
        with path.open(errors="replace") as source:
            for line in source:
                try:
                    row = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(row, dict):
                    yield row


def canonical_players(players: Any) -> tuple[tuple[str, float, str], ...]:
    result = []
    for player in players or []:
        player_id = str(player.get("player_id", ""))
        if player_id:
            result.append(
                (player_id, float(player.get("profit", 0) or 0), str(player.get("cards", "") or ""))
            )
    return tuple(sorted(result))


@dataclass
class StartInfo:
    first_time: datetime
    profiles: dict[str, str] = field(default_factory=dict)
    player_variants: Counter[tuple[tuple[str, str, float], ...]] = field(default_factory=Counter)
    bb_votes: Counter[float] = field(default_factory=Counter)
    sb_votes: Counter[float] = field(default_factory=Counter)
    ante_votes: Counter[float] = field(default_factory=Counter)
    straddle_votes: Counter[float] = field(default_factory=Counter)

    def add(self, when: datetime, hero: str, payload: dict[str, Any]) -> None:
        self.first_time = min(self.first_time, when)
        self.profiles[hero] = str(payload.get("ai_profile", ""))
        players = tuple(
            sorted(
                (
                    str(player.get("player_id", "")),
                    str(player.get("role", "") or ""),
                    float(player.get("stack", 0) or 0),
                )
                for player in payload.get("players", [])
                if player.get("player_id")
            )
        )
        if players:
            self.player_variants[players] += 1
        for key, target in (
            ("big_blind", self.bb_votes),
            ("small_blind", self.sb_votes),
            ("ante", self.ante_votes),
            ("straddle", self.straddle_votes),
        ):
            value = payload.get(key)
            if value is not None:
                target[float(value)] += 1

    @staticmethod
    def choice(votes: Counter[Any], default: Any = None) -> Any:
        if not votes:
            return default
        return sorted(votes.items(), key=lambda item: (-item[1], str(item[0])))[0][0]

    @property
    def players(self) -> tuple[tuple[str, str, float], ...]:
        return self.choice(self.player_variants, ())

    @property
    def bb(self) -> float | None:
        return self.choice(self.bb_votes)


@dataclass
class Metric:
    hands: set[tuple[str, str]] = field(default_factory=set)
    assignments: int = 0
    wins: int = 0
    losses: int = 0
    ties: int = 0
    chips: float = 0
    profit_bb: float = 0

    def add(self, key: tuple[str, str], profit: float, bb: float) -> None:
        self.hands.add(key)
        self.assignments += 1
        self.chips += profit
        self.profit_bb += profit / bb
        if profit > 0:
            self.wins += 1
        elif profit < 0:
            self.losses += 1
        else:
            self.ties += 1


@dataclass
class Hand:
    key: tuple[str, str]
    start: StartInfo
    settlements: tuple[tuple[str, float, str], ...]
    bb: float
    robots: list[tuple[str, float, str]]
    humans: list[tuple[str, float, str]]
    robot_participants: set[str]
    profiles_complete: bool
    profile_category: str

    @property
    def robot_profit(self) -> float:
        return sum(player[1] for player in self.robots)

    @property
    def human_profit(self) -> float:
        return sum(player[1] for player in self.humans)


def profile_category(profiles: list[str]) -> str:
    aggressive = profiles.count("FPCH_100_50")
    if aggressive == 1:
        return "exactly_one_aggressive"
    if aggressive > 1:
        return "multiple_aggressive"
    if profiles and all(profile == "FPCH_0_0" for profile in profiles):
        return "all_check_or_fold"
    if "FPCH_0_0" in profiles:
        return "mixed_check_or_fold_and_normal"
    return "no_special_profile"


def fmt(value: float, digits: int = 2) -> str:
    return f"{value:.{digits}f}"


def first_pass(files: list[Path], known_robot_ids: set[str]) -> dict[str, Any]:
    replay_robot_ids: set[str] = set()
    raw_starts: dict[tuple[str, str, int], StartInfo] = {}
    start_tokens: dict[tuple[str, str], list[int]] = defaultdict(list)
    raw_ends: dict[
        tuple[str, str, int], Counter[tuple[tuple[str, float, str], ...]]
    ] = defaultdict(Counter)
    unmatched_ends: list[
        tuple[str, str, datetime, tuple[tuple[str, float, str], ...]]
    ] = []
    outcome_counts: Counter[str] = Counter()
    rows = 0
    policies: Counter[str] = Counter()

    for row in iter_records(files):
        rows += 1
        outcome_counts[str(row.get("outcome", ""))] += 1
        policies[str(row.get("policy_version", ""))] += 1
        event = row.get("event") or {}
        hero = str(event.get("player_id", ""))
        if hero:
            replay_robot_ids.add(hero)
        if row.get("outcome") != "applied":
            continue
        table_id, hand_id = str(event.get("table_id", "")), str(event.get("hand_id", ""))
        if not table_id or not hand_id:
            continue
        base_key = (table_id, hand_id)
        cmd = event.get("cmd")
        payload = event.get("payload") or {}
        when = parse_time(str(row["recorded_at"]))
        if cmd == "start_hand_extended":
            payload_time = payload.get("time")
            candidate = (
                int((float(payload_time) + 500) // 1000)
                if payload_time is not None
                else int(when.timestamp() + 0.5)
            )
            nearby = [token for token in start_tokens[base_key] if abs(token - candidate) <= 2]
            token = min(nearby, key=lambda value: abs(value - candidate)) if nearby else candidate
            if not nearby:
                start_tokens[base_key].append(token)
            raw_key = (table_id, hand_id, token)
            info = raw_starts.get(raw_key)
            if info is None:
                info = raw_starts[raw_key] = StartInfo(first_time=when)
            info.add(when, hero, payload)
        elif cmd == "end_hand":
            players = canonical_players(payload.get("players"))
            if players:
                tokens = [
                    token
                    for token in start_tokens.get(base_key, [])
                    if token <= when.timestamp() + 2
                ]
                if tokens:
                    raw_ends[(table_id, hand_id, max(tokens))][players] += 1
                else:
                    unmatched_ends.append((table_id, hand_id, when, players))

    for table_id, hand_id, when, players in unmatched_ends:
        tokens = [
            token
            for token in start_tokens.get((table_id, hand_id), [])
            if token <= when.timestamp() + 2
        ]
        if tokens:
            raw_ends[(table_id, hand_id, max(tokens))][players] += 1

    sessions_by_table: dict[str, list[int]] = defaultdict(list)
    for table_id, hand_id, token in raw_starts:
        if hand_id == "1":
            sessions_by_table[table_id].append(token)
    for table_id in sessions_by_table:
        sessions_by_table[table_id] = sorted(set(sessions_by_table[table_id]))

    def instance_for(table_id: str, token: int) -> str | None:
        sessions = [session for session in sessions_by_table.get(table_id, []) if session <= token + 2]
        if not sessions:
            return None
        session = max(sessions)
        if len(sessions_by_table[table_id]) == 1:
            return table_id
        stamp = datetime.fromtimestamp(session, BEIJING).strftime("%Y%m%d-%H%M%S")
        return f"{table_id}@{stamp}"

    starts: dict[tuple[str, str], StartInfo] = {}
    ends: dict[tuple[str, str], Counter[tuple[tuple[str, float, str], ...]]] = defaultdict(Counter)
    room_created: dict[str, datetime] = {}
    event_key_lookup: dict[tuple[str, str], list[tuple[int, tuple[str, str]]]] = defaultdict(list)
    for (table_id, hand_id, token), info in raw_starts.items():
        instance = instance_for(table_id, token)
        if instance is None:
            continue
        key = (instance, hand_id)
        starts[key] = info
        event_key_lookup[(table_id, hand_id)].append((token, key))
        if hand_id == "1":
            room_created[instance] = info.first_time
    for (table_id, hand_id, token), variants in raw_ends.items():
        instance = instance_for(table_id, token)
        if instance is not None:
            ends[(instance, hand_id)].update(variants)
    for base_key in event_key_lookup:
        event_key_lookup[base_key].sort()

    return {
        "robot_ids": known_robot_ids | replay_robot_ids,
        "replay_robot_ids": replay_robot_ids,
        "starts": starts,
        "ends": ends,
        "room_created": room_created,
        "outcomes": outcome_counts,
        "policies": policies,
        "rows": rows,
        "event_key_lookup": event_key_lookup,
        "unmatched_end_records": len(unmatched_ends),
    }


def choose_end(variants: Counter[Any]) -> Any:
    return sorted(variants.items(), key=lambda item: (-item[1], str(item[0])))[0][0]


def build_hands(data: dict[str, Any], cutoff: datetime) -> tuple[list[Hand], dict[str, Any]]:
    robot_ids = data["robot_ids"]
    starts = data["starts"]
    room_created = data["room_created"]
    eligible = {table for table, when in room_created.items() if when > cutoff}
    result_tables = {key[0] for key in data["ends"]}
    missing_first = result_tables - set(room_created)
    before = set(room_created) - eligible
    table_bb_votes: dict[str, Counter[float]] = defaultdict(Counter)
    for (table, _), start in starts.items():
        if start.bb:
            table_bb_votes[table][start.bb] += 1
    table_bbs = {table: StartInfo.choice(votes) for table, votes in table_bb_votes.items()}

    hands = []
    missing_start = missing_bb = conflicts = 0
    for key, variants in data["ends"].items():
        if key[0] not in eligible:
            continue
        if len(variants) > 1:
            conflicts += 1
        start = starts.get(key)
        if start is None:
            missing_start += 1
            continue
        bb = start.bb or table_bbs.get(key[0])
        if not bb:
            missing_bb += 1
            continue
        settlement = choose_end(variants)
        robots = [player for player in settlement if player[0] in robot_ids]
        humans = [player for player in settlement if player[0] not in robot_ids]
        participant_ids = {player[0] for player in start.players} | {player[0] for player in settlement}
        robot_participants = participant_ids & robot_ids
        complete = robot_participants <= set(start.profiles)
        profiles = [start.profiles[player] for player in sorted(robot_participants) if player in start.profiles]
        hands.append(
            Hand(
                key=key,
                start=start,
                settlements=settlement,
                bb=bb,
                robots=robots,
                humans=humans,
                robot_participants=robot_participants,
                profiles_complete=complete,
                profile_category=profile_category(profiles) if complete else "incomplete",
            )
        )
    hands.sort(key=lambda hand: (hand.start.first_time, hand.key))
    return hands, {
        "eligible": eligible,
        "before": before,
        "missing_first": missing_first,
        "missing_start": missing_start,
        "missing_bb": missing_bb,
        "conflicts": conflicts,
        "table_bbs": table_bbs,
    }


def collect_top_history(
    files: list[Path],
    keys: set[tuple[str, str]],
    event_key_lookup: dict[tuple[str, str], list[tuple[int, tuple[str, str]]]],
) -> dict[tuple[str, str], dict[str, Any]]:
    result: dict[tuple[str, str], dict[str, Any]] = defaultdict(
        lambda: {"cards": {}, "boards": {}, "actions": defaultdict(list), "shows": {}}
    )
    for row in iter_records(files):
        if row.get("outcome") != "applied":
            continue
        event = row.get("event") or {}
        base_key = (str(event.get("table_id", "")), str(event.get("hand_id", "")))
        when = parse_time(str(row["recorded_at"]))
        candidates = [
            (token, key)
            for token, key in event_key_lookup.get(base_key, [])
            if token <= when.timestamp() + 2
        ]
        if not candidates:
            continue
        key = max(candidates, key=lambda item: item[0])[1]
        if key not in keys:
            continue
        cmd, payload = event.get("cmd"), event.get("payload") or {}
        seq = int(event.get("seq_num", 0) or 0)
        if cmd == "deal_cards" and payload.get("cards"):
            result[key]["cards"][str(event.get("player_id", ""))] = str(payload["cards"])
        elif cmd in {"flop", "turn", "river"}:
            result[key]["boards"][cmd] = str(payload.get("cards", ""))
        elif cmd == "show_cards" and payload.get("player_id"):
            result[key]["shows"][str(payload["player_id"])] = str(payload.get("cards", ""))
        elif cmd == "action":
            # The same table action is broadcast to several robot instances.
            # legal_actions can differ by recipient, so deduplicate on the
            # authoritative action fields and a tight recorded-time cluster.
            signature = (
                str(payload.get("player_id", "")),
                str(payload.get("type", "")),
                float(payload.get("value", 0) or 0),
                payload.get("stack_after"),
                str(payload.get("next_player_id", "")),
            )
            clusters = result[key]["actions"][signature]
            duplicate = next(
                (index for index, item in enumerate(clusters) if abs((when - item[0]).total_seconds()) < 1),
                None,
            )
            if duplicate is None:
                clusters.append((when, seq, payload))
            else:
                old_when, old_seq, old_payload = clusters[duplicate]
                clusters[duplicate] = (min(old_when, when), min(old_seq, seq), old_payload)
    return result


def analyze(args: argparse.Namespace) -> dict[str, Any]:
    root = args.input_root.resolve()
    output = args.output_dir.resolve()
    cutoff = parse_cutoff(args.cutoff)
    files = discover(root)
    if not files:
        raise SystemExit(f"no replay JSONL found under {root}")
    known_robot_ids = scan_nohup_robot_ids(root)
    data = first_pass(files, known_robot_ids)
    hands, scope = build_hands(data, cutoff)
    if not hands:
        raise SystemExit("no eligible settled hands")

    bb_robot: dict[float, Metric] = defaultdict(Metric)
    bb_human: dict[float, Metric] = defaultdict(Metric)
    profile_metrics: dict[tuple[float, str], Metric] = defaultdict(Metric)
    player_metrics: dict[tuple[float, str], Metric] = defaultdict(Metric)
    table_robot: dict[str, Metric] = defaultdict(Metric)
    table_human: dict[str, Metric] = defaultdict(Metric)
    table_robot_ids: dict[str, set[str]] = defaultdict(set)
    table_human_ids: dict[str, set[str]] = defaultdict(set)
    table_human_hands: dict[str, set[tuple[str, str]]] = defaultdict(set)
    profile_audit: Counter[str] = Counter()
    anomaly_rows = []
    anomaly_vs_human: dict[float, dict[str, Any]] = defaultdict(lambda: {
        "hands": 0, "rw": 0, "rl": 0, "rt": 0, "robot_bb": 0.0,
        "human_n": 0, "human_bb": 0.0,
    })
    fpch_loss: dict[float, dict[str, Any]] = defaultdict(lambda: {
        "vs_human": 0, "direct_n": 0, "direct_bb": 0.0, "direct_human_n": 0,
        "direct_human_bb": 0.0, "overall_n": 0, "overall_bb": 0.0,
    })
    fpch_loss_hands: list[tuple[Hand, set[str], float, bool, bool]] = []

    for hand in hands:
        key, bb = hand.key, hand.bb
        profile_audit[hand.profile_category] += 1
        if hand.profile_category not in {"exactly_one_aggressive", "all_check_or_fold"}:
            anomaly_rows.append(hand)
        for player_id, profit, _ in hand.robots:
            bb_robot[bb].add(key, profit, bb)
            player_metrics[(bb, player_id)].add(key, profit, bb)
            table_robot[hand.key[0]].add(key, profit, bb)
            profile = hand.start.profiles.get(player_id, "UNKNOWN_MISSING_START")
            profile_metrics[(bb, profile)].add(key, profit, bb)
            table_robot_ids[hand.key[0]].add(player_id)
        for player_id, profit, _ in hand.humans:
            bb_human[bb].add(key, profit, bb)
            table_human[hand.key[0]].add(key, profit, bb)
            table_human_ids[hand.key[0]].add(player_id)
        if hand.humans:
            table_human_hands[hand.key[0]].add(key)

        abnormal = hand.profiles_complete and hand.profile_category not in {
            "exactly_one_aggressive", "all_check_or_fold"
        }
        if abnormal and hand.humans:
            row = anomaly_vs_human[bb]
            row["hands"] += 1
            robot_profit = hand.robot_profit
            row["rw" if robot_profit > 0 else "rl" if robot_profit < 0 else "rt"] += 1
            row["robot_bb"] += robot_profit / bb
            row["human_n"] += len(hand.humans)
            row["human_bb"] += hand.human_profit / bb

        aggressive_ids = {
            player for player, profile in hand.start.profiles.items() if profile == "FPCH_100_50"
        }
        if aggressive_ids and hand.humans:
            row = fpch_loss[bb]
            row["vs_human"] += 1
            aggressive_profit = sum(profit for player, profit, _ in hand.robots if player in aggressive_ids)
            if aggressive_profit < 0 and hand.human_profit > 0:
                row["direct_n"] += 1
                row["direct_bb"] += aggressive_profit / bb
                row["direct_human_n"] += len(hand.humans)
                row["direct_human_bb"] += hand.human_profit / bb
            if hand.robot_profit < 0 and hand.human_profit > 0:
                row["overall_n"] += 1
                row["overall_bb"] += hand.robot_profit / bb
            direct_loss = aggressive_profit < 0 and hand.human_profit > 0
            overall_loss = hand.robot_profit < 0 and hand.human_profit > 0
            if direct_loss or overall_loss:
                fpch_loss_hands.append(
                    (hand, aggressive_ids, aggressive_profit, direct_loss, overall_loss)
                )

    top = sorted(hands, key=lambda hand: (hand.robot_profit / hand.bb, hand.key))[: args.top]
    histories = collect_top_history(
        files, {hand.key for hand in top}, data["event_key_lookup"]
    )
    output.mkdir(parents=True, exist_ok=True)
    prefix = args.report_prefix or f"profit-ainp-replay-post-{cutoff:%Y%m%d-%H%M%S}"
    analysis_path = output / f"{prefix}-analysis.md"
    players_path = output / f"{prefix}-players.md"
    top_path = output / f"{prefix}-top{args.top}-hands.md"
    anomaly_path = output / f"{prefix}-profile-anomalies.md"
    fpch_loss_path = output / f"{prefix}-fpch10050-loss-vs-human.md"
    daily_trend_path = output / f"{prefix}-daily-trend.md"

    bbs = sorted(bb_robot)
    report = [
        f"# ainp_log replay：{cutoff.isoformat()} 后新房盈利分析", "",
        "## 统计口径", "",
        f"- 输入：`{root}` 下 {len(files)} 个 replay JSONL；盈利与结算只读取 replay。",
        "- 各实例按“第1手划分的牌桌实例 + hand_id”合并；重复使用的短桌号不会串桌，同一手结算多副本采用多数版本。",
        f"- 仅纳入明确观察到第1手、且首手记录时间严格晚于北京时间 `{cutoff.isoformat()}` 的房间。",
        "- 真人标记按全部实例混合确认：扫描同目录所有 nohup 的机器人接收者 ID，再与 replay 完整结算玩家做差集；nohup 不参与盈利计算。",
        "- 真人参局次数按玩家计：同一手有多个真人时分别计数。机器人 BB/100 按物理手数，真人 BB/100 按真人参局次数。", "",
        "## 数据范围", "",
        f"- replay 记录 {data['rows']} 条；策略版本：`{', '.join(k for k in data['policies'] if k)}`。",
        f"- 有第1手的房间 {len(data['room_created'])} 个：纳入 {len(scope['eligible'])} 个，截止前排除 {len(scope['before'])} 个。",
        f"- 另有 {len(scope['missing_first'])} 个出现结算但没有第1手记录的房间，排除。",
        f"- 纳入 {len(hands)} 手；缺开局 {scope['missing_start']} 手、缺BB {scope['missing_bb']} 手；结算存在多版本 {scope['conflicts']} 手。",
        f"- 全部日志识别机器人ID {len(data['robot_ids'])} 个；纳入牌局实际出现机器人 {len({p[0] for h in hands for p in h.robots})} 个、真人 {len({p[0] for h in hands for p in h.humans})} 个。",
        f"- 最早纳入房间：{min(data['room_created'][t] for t in scope['eligible']).isoformat()}；最晚：{max(data['room_created'][t] for t in scope['eligible']).isoformat()}。", "",
        "## 分 BB 总体盈利", "",
        "| BB | 房间 | 已结算手数 | 机器人结算次数 | 机器人净盈利BB | 机器人BB/100 | 真人参局次数 | 真人胜/负/平 | 真人净盈利BB | 真人BB/100 | 对真人物理手数 | 全机器人手数 |",
        "|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for bb in bbs:
        rm, hm = bb_robot[bb], bb_human[bb]
        rooms = {h.key[0] for h in hands if h.bb == bb}
        vs_human = len({h.key for h in hands if h.bb == bb and h.humans})
        report.append(
            f"| {bb:g} | {len(rooms)} | {len(rm.hands)} | {rm.assignments} | {fmt(rm.profit_bb,4)} | "
            f"{fmt(rm.profit_bb / len(rm.hands) * 100,2)} | {hm.assignments} | {hm.wins}/{hm.losses}/{hm.ties} | "
            f"{fmt(hm.profit_bb,4)} | {fmt(hm.profit_bb / hm.assignments * 100 if hm.assignments else 0,2)} | "
            f"{vs_human} | {len(rm.hands)-vs_human} |"
        )

    report += ["", "### 对真人与全机器人牌局拆分", "",
        "纯机器人牌局的合计净亏主要反映抽水；对真人牌局单独列出，避免将两者混为策略输赢。", "",
        "| BB | 对真人手数 | 机器人对真人净盈利BB | 机器人对真人BB/100 | 真人净盈利BB | 全机器人手数 | 全机器人合计净盈利BB |",
        "|---:|---:|---:|---:|---:|---:|---:|"]
    for bb in bbs:
        vs_human_hands = [hand for hand in hands if hand.bb == bb and hand.humans]
        robot_vs_human = sum(hand.robot_profit / bb for hand in vs_human_hands)
        human_profit = sum(hand.human_profit / bb for hand in vs_human_hands)
        robot_only_hands = [hand for hand in hands if hand.bb == bb and not hand.humans]
        robot_only_profit = sum(hand.robot_profit / bb for hand in robot_only_hands)
        report.append(
            f"| {bb:g} | {len(vs_human_hands)} | {fmt(robot_vs_human,4)} | "
            f"{fmt(robot_vs_human/len(vs_human_hands)*100 if vs_human_hands else 0,2)} | "
            f"{fmt(human_profit,4)} | {len(robot_only_hands)} | {fmt(robot_only_profit,4)} |"
        )

    report += ["", "## 分 BB、分风格", "",
        "| BB | ai_profile | 手数 | 结算次数 | 胜/负/平 | 净盈利筹码 | 净盈利BB | 每100次结算BB |",
        "|---:|---|---:|---:|---:|---:|---:|---:|"]
    for (bb, profile), metric in sorted(profile_metrics.items()):
        report.append(
            f"| {bb:g} | {profile} | {len(metric.hands)} | {metric.assignments} | {metric.wins}/{metric.losses}/{metric.ties} | "
            f"{fmt(metric.chips)} | {fmt(metric.profit_bb,4)} | {fmt(metric.profit_bb / metric.assignments * 100,2)} |"
        )

    complete = len(hands) - profile_audit["incomplete"]
    passed = profile_audit["exactly_one_aggressive"] + profile_audit["all_check_or_fold"]
    report += ["", "## ai_profile 强约束核验", "",
        f"- 共观察 {len(hands)} 手；画像记录完整 {complete} 手，通过 {passed} 手，完整样本通过率 {passed / complete * 100 if complete else 0:.4f}%。",
        f"- 画像记录不完整：{profile_audit['incomplete']} 手。",
        f"- 恰好一个 `FPCH_100_50`：{profile_audit['exactly_one_aggressive']} 手。",
        f"- 全部机器人均为 `FPCH_0_0`：{profile_audit['all_check_or_fold']} 手。",
        f"- 多个 `FPCH_100_50`：{profile_audit['multiple_aggressive']} 手。",
        f"- 无100_50且0_0混普通风格：{profile_audit['mixed_check_or_fold_and_normal']} 手。",
        f"- 无100_50且没有0_0：{profile_audit['no_special_profile']} 手。", "",
        "## 异常画像牌局对真人汇总", "",
        "| BB | 对真人物理手数 | 机器人整手胜/负/平 | 机器人净盈利BB | 机器人BB/100 | 真人参局次数 | 真人净盈利BB | 真人BB/100 |",
        "|---:|---:|---:|---:|---:|---:|---:|---:|"]
    for bb in bbs:
        row = anomaly_vs_human[bb]
        report.append(
            f"| {bb:g} | {row['hands']} | {row['rw']}/{row['rl']}/{row['rt']} | {fmt(row['robot_bb'],4)} | "
            f"{fmt(row['robot_bb']/row['hands']*100 if row['hands'] else 0,2)} | {row['human_n']} | "
            f"{fmt(row['human_bb'],4)} | {fmt(row['human_bb']/row['human_n']*100 if row['human_n'] else 0,2)} |"
        )

    report += ["", "## 存在 FPCH_100_50 但输给真人", "",
        f"- 有真人参与且存在 `FPCH_100_50`：{sum(row['vs_human'] for row in fpch_loss.values())} 手。",
        f"- `FPCH_100_50` 机器人本人净亏、且真人合计盈利：{sum(row['direct_n'] for row in fpch_loss.values())} 手。",
        f"- 该手所有机器人合计净亏、且真人合计盈利：{sum(row['overall_n'] for row in fpch_loss.values())} 手。", "",
        "| BB | 含100_50的对真人手数 | 100_50本人亏损手数/占比 | 100_50本人净亏BB | 对应真人参局次数 | 对应真人净盈利BB | 机器人整体亏损手数/占比 | 机器人整体净亏BB |",
        "|---:|---:|---:|---:|---:|---:|---:|---:|"]
    for bb in bbs:
        row = fpch_loss[bb]
        report.append(
            f"| {bb:g} | {row['vs_human']} | {row['direct_n']}/{row['direct_n']/row['vs_human']*100 if row['vs_human'] else 0:.2f}% | "
            f"{fmt(row['direct_bb'],4)} | {row['direct_human_n']} | {fmt(row['direct_human_bb'],4)} | "
            f"{row['overall_n']}/{row['overall_n']/row['vs_human']*100 if row['vs_human'] else 0:.2f}% | {fmt(row['overall_bb'],4)} |"
        )

    report += ["", "## 分 BB、分牌桌盈利", "",
        "| BB | 牌桌 | 首手时间（北京时间） | 手数 | 机器人/真人人数 | 机器人净盈利BB | 机器人BB/100 | 真人参局次数 | 真人净盈利BB | 真人BB/100 |",
        "|---:|---|---|---:|---:|---:|---:|---:|---:|---:|"]
    for table in sorted(scope["eligible"], key=lambda t: (scope["table_bbs"].get(t, 0), t)):
        rm, hm = table_robot.get(table), table_human.get(table, Metric())
        if rm is None:
            continue
        bb = scope["table_bbs"].get(table)
        report.append(
            f"| {bb:g} | {table} | {data['room_created'][table].isoformat()} | {len(rm.hands)} | "
            f"{len(table_robot_ids[table])}/{len(table_human_ids[table])} | {fmt(rm.profit_bb,4)} | "
            f"{fmt(rm.profit_bb/len(rm.hands)*100,2)} | {hm.assignments} | {fmt(hm.profit_bb,4)} | "
            f"{fmt(hm.profit_bb/hm.assignments*100 if hm.assignments else 0,2)} |"
        )
    analysis_path.write_text("\n".join(report) + "\n")

    player_report = [f"# ainp_log replay：{cutoff.isoformat()} 后机器人明细", ""]
    for bb in bbs:
        player_report += [f"## BB={bb:g}", "", "| 机器人ID | 手数 | 胜/负/平 | 净盈利BB | BB/100 |", "|---|---:|---:|---:|---:|"]
        rows = sorted(((p, m) for (b, p), m in player_metrics.items() if b == bb), key=lambda x: (-x[1].profit_bb, x[0]))
        for player, metric in rows:
            player_report.append(f"| {player} | {metric.assignments} | {metric.wins}/{metric.losses}/{metric.ties} | {fmt(metric.profit_bb,4)} | {fmt(metric.profit_bb/metric.assignments*100,2)} |")
        player_report.append("")
    players_path.write_text("\n".join(player_report) + "\n")

    anomaly_report = [f"# ainp_log replay：{cutoff.isoformat()} 后画像异常明细", "",
        "| 牌桌/手数 | 类型 | 机器人风格 | 真人ID | 机器人/真人净盈利BB |",
        "|---|---|---|---|---:|"]
    for hand in anomaly_rows:
        profiles = "; ".join(f"{p}:{hand.start.profiles.get(p,'UNKNOWN')}" for p in sorted(hand.robot_participants))
        human_ids = ", ".join(p[0] for p in hand.humans) or "无"
        anomaly_report.append(f"| {hand.key[0]} / {hand.key[1]} | {hand.profile_category} | {profiles} | {human_ids} | {fmt(hand.robot_profit/hand.bb,4)} / {fmt(hand.human_profit/hand.bb,4)} |")
    anomaly_path.write_text("\n".join(anomaly_report) + "\n")

    fpch_report = [
        f"# ainp_log replay：{cutoff.isoformat()} 后 FPCH_100_50 输给真人明细", "",
        "纳入存在 `FPCH_100_50`、真人合计盈利，并且 `FPCH_100_50` 本人或全部机器人合计亏损的牌局。", "",
        "| BB | 牌桌/手数 | 时间 | 100_50机器人 | 100_50净盈利BB | 机器人合计BB | 真人ID | 真人合计BB | 本人亏/整体亏 |",
        "|---:|---|---|---|---:|---:|---|---:|---|",
    ]
    for hand, aggressive_ids, aggressive_profit, direct_loss, overall_loss in sorted(
        fpch_loss_hands, key=lambda row: (row[0].bb, row[0].key)
    ):
        human_ids = ", ".join(player[0] for player in hand.humans)
        label = f"{'是' if direct_loss else '否'}/{'是' if overall_loss else '否'}"
        fpch_report.append(
            f"| {hand.bb:g} | {hand.key[0]} / {hand.key[1]} | {hand.start.first_time.isoformat()} | "
            f"{', '.join(sorted(aggressive_ids))} | {fmt(aggressive_profit/hand.bb,4)} | "
            f"{fmt(hand.robot_profit/hand.bb,4)} | {human_ids} | {fmt(hand.human_profit/hand.bb,4)} | {label} |"
        )
    fpch_loss_path.write_text("\n".join(fpch_report) + "\n")

    # Daily cohorts are based on the room's first-hand date.  This keeps a room
    # intact when play crosses midnight and matches the room-creation cutoff.
    daily_hands: dict[str, list[Hand]] = defaultdict(list)
    for hand in hands:
        created_date = data["room_created"][hand.key[0]].date().isoformat()
        daily_hands[created_date].append(hand)

    daily_report = [
        f"# ainp_log replay：{cutoff.isoformat()} 后按房间创建日趋势", "",
        "## 统计口径", "",
        "- 每桌按第一手的北京时间日期归组；跨午夜继续运行的牌局仍全部归属于创建日。",
        "- 不同BB不混合；机器人整体BB/100按物理手数，真人BB/100按真人参局次数。",
        "- 盈利与结算只取replay；真人身份继续使用全部nohup混合确认。", "",
        "## 每日汇总", "",
        "| 日期 | BB | 房间 | 手数 | 机器人净盈利BB | 机器人整体BB/100 | 对真人手数 | 机器人对真人BB | 机器人对真人BB/100 | 真人参局次数 | 真人胜/负/平 | 真人净盈利BB | 真人BB/100 | 全机器人手数 |",
        "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    daily_rows: dict[str, list[str]] = defaultdict(list)
    for date in sorted(daily_hands):
        for bb in sorted({hand.bb for hand in daily_hands[date]}):
            selected = [hand for hand in daily_hands[date] if hand.bb == bb]
            rooms = {hand.key[0] for hand in selected}
            robot_bb = sum(hand.robot_profit / bb for hand in selected)
            vs_human = [hand for hand in selected if hand.humans]
            robot_vs_human_bb = sum(hand.robot_profit / bb for hand in vs_human)
            humans = [player for hand in selected for player in hand.humans]
            human_bb = sum(player[1] / bb for player in humans)
            human_wins = sum(player[1] > 0 for player in humans)
            human_losses = sum(player[1] < 0 for player in humans)
            human_ties = sum(player[1] == 0 for player in humans)
            row = (
                f"| {date} | {bb:g} | {len(rooms)} | {len(selected)} | {fmt(robot_bb,4)} | "
                f"{fmt(robot_bb/len(selected)*100,2)} | {len(vs_human)} | {fmt(robot_vs_human_bb,4)} | "
                f"{fmt(robot_vs_human_bb/len(vs_human)*100 if vs_human else 0,2)} | {len(humans)} | "
                f"{human_wins}/{human_losses}/{human_ties} | {fmt(human_bb,4)} | "
                f"{fmt(human_bb/len(humans)*100 if humans else 0,2)} | {len(selected)-len(vs_human)} |"
            )
            daily_report.append(row)
            daily_rows[date].append(row)

    daily_report += ["", "## 趋势阅读", "",
        "- 对真人BB/100用于观察策略效果；整体BB/100还包含纯机器人牌局抽水。",
        "- 真人样本较少的日期或BB档位波动会很大，应同时查看对真人手数和真人参局次数。",
    ]
    daily_trend_path.write_text("\n".join(daily_report) + "\n")

    daily_paths = []
    for date in sorted(daily_rows):
        daily_path = output / f"{prefix}-daily-{date}.md"
        daily_path.write_text(
            "\n".join(
                [
                    f"# ainp_log replay：{date} 创建房间盈利统计", "",
                    "房间按第一手北京时间日期归组；不同BB不混合。", "",
                    "| 日期 | BB | 房间 | 手数 | 机器人净盈利BB | 机器人整体BB/100 | 对真人手数 | 机器人对真人BB | 机器人对真人BB/100 | 真人参局次数 | 真人胜/负/平 | 真人净盈利BB | 真人BB/100 | 全机器人手数 |",
                    "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
                    *daily_rows[date], "",
                ]
            )
        )
        daily_paths.append(str(daily_path))

    top_report = [f"# ainp_log replay：{cutoff.isoformat()} 后机器人亏损最大{args.top}手", "", "## 排名", "",
        "| 排名 | BB | 牌桌/手数 | 机器人净盈利BB | 真人净盈利BB | 真人数 |",
        "|---:|---:|---|---:|---:|---:|"]
    for index, hand in enumerate(top, 1):
        top_report.append(f"| {index} | {hand.bb:g} | {hand.key[0]} / {hand.key[1]} | {fmt(hand.robot_profit/hand.bb,4)} | {fmt(hand.human_profit/hand.bb,4)} | {len(hand.humans)} |")
    for index, hand in enumerate(top, 1):
        hist = histories[hand.key]
        start_players = {p[0]: p for p in hand.start.players}
        end_cards = {p[0]: p[2] for p in hand.settlements if p[2]}
        cards = {**hist["shows"], **end_cards, **hist["cards"]}
        board = " ".join(hist["boards"].get(street, "") for street in ("flop", "turn", "river")).strip() or "未知"
        top_report += ["", f"## {index}. 牌桌 {hand.key[0]}，第 {hand.key[1]} 手", "",
            f"- BB：{hand.bb:g}", f"- 时间：{hand.start.first_time.isoformat()}",
            f"- 公共牌：`{board}`", f"- 机器人合计：**{fmt(hand.robot_profit)} 筹码（{fmt(hand.robot_profit/hand.bb,4)} BB）**",
            f"- 真人合计：**{fmt(hand.human_profit)} 筹码（{fmt(hand.human_profit/hand.bb,4)} BB）**", "", "### 玩家与结算", ""]
        for player_id, profit, _ in hand.settlements:
            kind = "机器人" if player_id in data["robot_ids"] else "真人"
            profile = hand.start.profiles.get(player_id, "-")
            role = start_players.get(player_id, ("", "", 0))[1] or "-"
            stack = start_players.get(player_id, ("", "", 0))[2]
            top_report.append(f"- {player_id}（{kind}，{profile}，{role}，起始{stack:g}）：手牌`{cards.get(player_id,'未知') or '未知'}`，结算{profit:+g}（{profit/hand.bb:+.4f} BB）。")
        top_report += ["", "### 动作", ""]
        actions = sorted(
            (item for clusters in hist["actions"].values() for item in clusters),
            key=lambda item: (item[0], item[1]),
        )
        if not actions:
            top_report.append("- 无动作记录。")
        for _, seq, payload in actions:
            value = float(payload.get("value", 0) or 0)
            stack_after = payload.get("stack_after")
            suffix = f"，操作后筹码{float(stack_after):g}" if stack_after is not None else ""
            top_report.append(f"- seq {seq}：{payload.get('player_id')} {payload.get('type')} {value:g}{suffix}。")
    top_path.write_text("\n".join(top_report) + "\n")

    return {
        "files": len(files), "records": data["rows"], "eligible_rooms": len(scope["eligible"]),
        "hands": len(hands), "robots": len(data["robot_ids"]),
        "humans": len({p[0] for h in hands for p in h.humans}),
        "outputs": [
            str(analysis_path), str(players_path), str(top_path), str(anomaly_path),
            str(fpch_loss_path), str(daily_trend_path), *daily_paths,
        ],
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    base = Path(__file__).resolve().parent.parent
    parser.add_argument("--input-root", type=Path, default=base / "build" / "ainp_log")
    parser.add_argument("--cutoff", default="2026-09-19 22:55:00")
    parser.add_argument("--output-dir", type=Path, default=base / "reports")
    parser.add_argument("--report-prefix")
    parser.add_argument("--top", type=int, default=20)
    args = parser.parse_args()
    if args.top < 1:
        parser.error("--top must be positive")
    return args


if __name__ == "__main__":
    print(json.dumps(analyze(parse_args()), ensure_ascii=False, indent=2))
