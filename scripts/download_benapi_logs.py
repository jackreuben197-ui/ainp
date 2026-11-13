#!/usr/bin/env python3
"""Incrementally download selected service logs exposed by logstorage."""

from __future__ import annotations

import argparse
import concurrent.futures
import dataclasses
import email.utils
import fcntl
import json
import os
import re
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path, PurePosixPath


DEFAULT_BASE_URL = "https://logstorage.elatedbuck.com"
DEFAULT_RESERVE_BYTES = 2 * 1024**3


@dataclasses.dataclass(frozen=True)
class RemoteFile:
    path: PurePosixPath
    size: int
    mtime: str


def format_bytes(value: int) -> str:
    units = ("B", "KiB", "MiB", "GiB", "TiB")
    size = float(value)
    for unit in units:
        if size < 1024 or unit == units[-1]:
            return f"{size:.2f} {unit}"
        size /= 1024
    return f"{size:.2f} TiB"


class LogStorageClient:
    def __init__(self, base_url: str, timeout: float, retries: int) -> None:
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.retries = retries

    def _url(self, prefix: str, path: PurePosixPath | None = None) -> str:
        parts = [] if path is None else list(path.parts)
        encoded = "/".join(urllib.parse.quote(part, safe="") for part in parts)
        suffix = f"{encoded}/" if encoded else ""
        return f"{self.base_url}/{prefix}/{suffix}"

    def list_directory(self, path: PurePosixPath | None) -> list[dict[str, object]]:
        url = self._url("api/list", path)
        last_error: Exception | None = None
        for attempt in range(1, self.retries + 1):
            try:
                request = urllib.request.Request(url, headers={"User-Agent": "ainp-log-downloader/1.0"})
                with urllib.request.urlopen(request, timeout=self.timeout) as response:
                    payload = json.load(response)
                if not isinstance(payload, list):
                    raise ValueError(f"API did not return a list: {url}")
                return payload
            except (OSError, ValueError, urllib.error.URLError) as error:
                last_error = error
                if attempt < self.retries:
                    time.sleep(min(2 ** (attempt - 1), 8))
        raise RuntimeError(f"failed to list {url}: {last_error}")

    def download_url(self, path: PurePosixPath) -> str:
        return self._url("dl", path).rstrip("/")


def safe_name(name: object) -> str:
    if not isinstance(name, str) or not name or name in {".", ".."}:
        raise ValueError(f"unsafe remote name: {name!r}")
    if "/" in name or "\\" in name or "\x00" in name:
        raise ValueError(f"unsafe remote name: {name!r}")
    return name


def matches_service_name(
    name: str,
    prefix: str,
    minimum_index: int | None = None,
    maximum_index: int | None = None,
    index_digits: int | None = None,
) -> bool:
    name, prefix = name.lower(), prefix.lower()
    if minimum_index is None and maximum_index is None:
        return name.startswith(prefix)
    digits = rf"\d{{{index_digits}}}" if index_digits is not None else r"\d+"
    match = re.fullmatch(re.escape(prefix) + rf"({digits})", name)
    if match is None:
        return False
    index = int(match.group(1))
    return (minimum_index is None or index >= minimum_index) and (
        maximum_index is None or index <= maximum_index
    )


def is_service_path(
    path: PurePosixPath,
    prefix: str,
    minimum_index: int | None = None,
    maximum_index: int | None = None,
    index_digits: int | None = None,
) -> bool:
    return any(
        matches_service_name(
            part, prefix, minimum_index, maximum_index, index_digits
        )
        for part in path.parts
    )


def discover_files(
    client: LogStorageClient,
    workers: int,
    service_prefix: str = "pokerbot",
    service_min: int | None = None,
    service_max: int | None = None,
    service_digits: int | None = None,
) -> list[RemoteFile]:
    files: list[RemoteFile] = []
    directories: list[tuple[PurePosixPath | None, int]] = [(None, 0)]
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
        while directories:
            futures = {
                executor.submit(client.list_directory, path): (path, depth)
                for path, depth in directories
            }
            next_directories: list[tuple[PurePosixPath, int]] = []
            for future in concurrent.futures.as_completed(futures):
                path, depth = futures[future]
                for entry in future.result():
                    name = safe_name(entry.get("name"))
                    child = PurePosixPath(name) if path is None else path / name
                    entry_type = entry.get("type")
                    if entry_type == "directory":
                        # Root entries are hosts. Their direct children must match the
                        # requested service range; avoid crawling unrelated services.
                        if depth == 1 and not matches_service_name(
                            name,
                            service_prefix,
                            service_min,
                            service_max,
                            service_digits,
                        ):
                            continue
                        next_directories.append((child, depth + 1))
                    elif entry_type == "file" and is_service_path(
                        child,
                        service_prefix,
                        service_min,
                        service_max,
                        service_digits,
                    ):
                        size = entry.get("size")
                        if not isinstance(size, int) or size < 0:
                            raise ValueError(f"invalid size for {child}: {size!r}")
                        files.append(RemoteFile(child, size, str(entry.get("mtime", ""))))
            directories = next_directories
    files.sort(key=lambda item: str(item.path))
    return files


def classify(files: list[RemoteFile], output: Path) -> tuple[list[RemoteFile], int, int]:
    pending: list[RemoteFile] = []
    skipped_bytes = 0
    pending_bytes = 0
    for item in files:
        destination = output.joinpath(*item.path.parts)
        if destination.is_file() and destination.stat().st_size == item.size:
            skipped_bytes += item.size
            continue
        pending.append(item)
        existing_part = destination.with_name(destination.name + ".part")
        reusable = existing_part.stat().st_size if existing_part.is_file() else 0
        # Older script versions could leave an incomplete final-path file. Reuse
        # it instead of downloading the same prefix again.
        if destination.is_file() and destination.stat().st_size < item.size:
            reusable = max(reusable, destination.stat().st_size)
        pending_bytes += max(0, item.size - min(reusable, item.size))
    return pending, skipped_bytes, pending_bytes


def set_mtime(path: Path, value: str) -> None:
    if not value:
        return
    parsed = email.utils.parsedate_to_datetime(value)
    timestamp = parsed.timestamp()
    os.utime(path, (timestamp, timestamp))


def download_one(
    client: LogStorageClient,
    item: RemoteFile,
    output: Path,
    timeout: float,
    retries: int,
) -> tuple[str, int, int]:
    destination = output.joinpath(*item.path.parts)
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_name(destination.name + ".part")

    # Promote the largest reusable incomplete copy to the .part path. Keeping
    # downloads on .part and replacing only after validation makes reruns safe.
    if destination.is_file():
        destination_size = destination.stat().st_size
        if destination_size == item.size:
            set_mtime(destination, item.mtime)
            return str(item.path), item.size, 0
        temporary_size = temporary.stat().st_size if temporary.is_file() else -1
        if destination_size < item.size and destination_size > temporary_size:
            os.replace(destination, temporary)

    if temporary.exists() and temporary.stat().st_size > item.size:
        # This cannot be resumed against the advertised remote size.
        temporary.unlink()
    if temporary.exists() and temporary.stat().st_size == item.size:
        os.replace(temporary, destination)
        set_mtime(destination, item.mtime)
        return str(item.path), item.size, 0

    starting_size = temporary.stat().st_size if temporary.is_file() else 0

    command = [
        "curl",
        "-fL",
        "--http1.1",
        "--silent",
        "--show-error",
        "--retry",
        str(retries),
        "--retry-all-errors",
        "--retry-delay",
        "3",
        "--retry-max-time",
        "300",
        "--connect-timeout",
        str(max(1, int(timeout))),
        "--speed-limit",
        "1024",
        "--speed-time",
        "60",
        "--continue-at",
        "-",
        "--output",
        str(temporary),
        client.download_url(item.path),
    ]
    subprocess.run(command, check=True)
    actual_size = temporary.stat().st_size
    if actual_size != item.size:
        raise RuntimeError(f"size mismatch for {item.path}: expected {item.size}, got {actual_size}")
    os.replace(temporary, destination)
    set_mtime(destination, item.mtime)
    return str(item.path), item.size, max(0, actual_size - starting_size)


def parse_args(
    argv: list[str] | None = None,
    *,
    default_output: Path | None = None,
    default_service_prefix: str = "pokerbot",
    default_service_min: int | None = None,
    default_service_max: int | None = None,
    default_service_digits: int | None = None,
    description: str | None = None,
) -> argparse.Namespace:
    script_dir = Path(__file__).resolve().parent
    if default_output is None:
        default_output = script_dir.parent / "build" / "benapi"
    parser = argparse.ArgumentParser(description=description or __doc__)
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL)
    parser.add_argument("--output", type=Path, default=default_output)
    parser.add_argument("--service-prefix", default=default_service_prefix)
    parser.add_argument("--service-min", type=int, default=default_service_min)
    parser.add_argument("--service-max", type=int, default=default_service_max)
    parser.add_argument("--service-digits", type=int, default=default_service_digits)
    parser.add_argument(
        "--workers",
        type=int,
        default=4,
        help="parallel list/download requests (default: 4, conservative for logstorage)",
    )
    parser.add_argument("--timeout", type=float, default=30)
    parser.add_argument("--retries", type=int, default=5)
    parser.add_argument(
        "--rounds",
        type=int,
        default=4,
        help="script-level retry rounds for files that still fail after curl retries",
    )
    parser.add_argument(
        "--round-backoff",
        type=float,
        default=20,
        help="initial seconds between retry rounds; doubles up to 120 seconds",
    )
    parser.add_argument("--reserve-gib", type=float, default=2.0)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument(
        "--ignore-space-check",
        action="store_true",
        help="download even when free-space preflight fails",
    )
    return parser.parse_args(argv)


def main(
    argv: list[str] | None = None,
    *,
    default_output: Path | None = None,
    default_service_prefix: str = "pokerbot",
    default_service_min: int | None = None,
    default_service_max: int | None = None,
    default_service_digits: int | None = None,
    description: str | None = None,
) -> int:
    args = parse_args(
        argv,
        default_output=default_output,
        default_service_prefix=default_service_prefix,
        default_service_min=default_service_min,
        default_service_max=default_service_max,
        default_service_digits=default_service_digits,
        description=description,
    )
    if (
        args.workers < 1
        or args.retries < 1
        or args.rounds < 1
        or args.round_backoff < 0
        or args.reserve_gib < 0
        or not args.service_prefix.strip()
        or (args.service_min is not None and args.service_min < 0)
        or (args.service_max is not None and args.service_max < 0)
        or (args.service_digits is not None and args.service_digits < 1)
        or (
            args.service_min is not None
            and args.service_max is not None
            and args.service_min > args.service_max
        )
    ):
        raise SystemExit(
            "workers/retries/rounds must be positive; "
            "round-backoff/reserve-gib/service bounds must be non-negative; "
            "service-prefix must be non-empty and service-min cannot exceed service-max"
        )
    if shutil.which("curl") is None:
        raise SystemExit("curl is required")

    output = args.output.expanduser().resolve()
    output.mkdir(parents=True, exist_ok=True)
    lock_path = output.parent / f".{output.name}-download.lock"
    lock_file = lock_path.open("a+")
    try:
        fcntl.flock(lock_file, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        raise SystemExit(f"another downloader is already using {output}")

    client = LogStorageClient(args.base_url, args.timeout, args.retries)
    service_range = args.service_prefix
    if args.service_min is not None or args.service_max is not None:
        service_range += f"[{args.service_min or 0}..{args.service_max or '∞'}]"
    print(
        f"Discovering {service_range} logs from {client.base_url} ...", flush=True
    )
    files = discover_files(
        client,
        args.workers,
        args.service_prefix,
        args.service_min,
        args.service_max,
        args.service_digits,
    )
    pending, skipped_bytes, pending_bytes = classify(files, output)
    total_bytes = sum(item.size for item in files)
    print(
        f"Remote: {len(files)} files, {format_bytes(total_bytes)}; "
        f"complete locally: {len(files) - len(pending)} files, {format_bytes(skipped_bytes)}; "
        f"pending: {len(pending)} files, {format_bytes(pending_bytes)}",
        flush=True,
    )
    if args.dry_run or not pending:
        return 0

    free_bytes = shutil.disk_usage(output).free
    reserve_bytes = int(args.reserve_gib * 1024**3)
    if not args.ignore_space_check and free_bytes < pending_bytes + reserve_bytes:
        print(
            f"Insufficient space: free {format_bytes(free_bytes)}, need "
            f"{format_bytes(pending_bytes + reserve_bytes)} including reserve. "
            "Free disk space or pass --ignore-space-check.",
            file=sys.stderr,
        )
        return 2

    completed_files = 0
    completed_bytes = 0
    transferred_bytes = 0
    remaining = pending
    failures: list[tuple[str, str]] = []
    for round_number in range(1, args.rounds + 1):
        failures = []
        failed_items: list[RemoteFile] = []
        print(
            f"Download round {round_number}/{args.rounds}: {len(remaining)} files",
            flush=True,
        )
        with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as executor:
            futures = {
                executor.submit(
                    download_one, client, item, output, args.timeout, args.retries
                ): item
                for item in remaining
            }
            for future in concurrent.futures.as_completed(futures):
                item = futures[future]
                try:
                    path, size, transferred = future.result()
                    print(
                        f"[{completed_files + 1}/{len(pending)}] {path} "
                        f"({format_bytes(size)}, transferred {format_bytes(transferred)})",
                        flush=True,
                    )
                    completed_files += 1
                    completed_bytes += size
                    transferred_bytes += transferred
                except Exception as error:  # keep downloading independent files
                    failures.append((str(item.path), str(error)))
                    failed_items.append(item)
                    print(f"FAILED {item.path}: {error}", file=sys.stderr, flush=True)

        if not failed_items:
            break
        remaining = failed_items
        if round_number < args.rounds:
            delay = min(args.round_backoff * (2 ** (round_number - 1)), 120)
            print(
                f"Retrying {len(remaining)} failed files in {delay:.0f}s ...",
                flush=True,
            )
            time.sleep(delay)

    print(
        f"Completed {completed_files} files ({format_bytes(completed_bytes)}); "
        f"transferred {format_bytes(transferred_bytes)}; "
        f"failed {len(failures)}.",
        flush=True,
    )
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
