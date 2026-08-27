#!/usr/bin/env python3
"""Incrementally download every pokerbot log exposed by logstorage."""

from __future__ import annotations

import argparse
import concurrent.futures
import dataclasses
import email.utils
import json
import os
import shutil
import subprocess
import sys
import threading
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


def is_pokerbot_path(path: PurePosixPath) -> bool:
    return any(part.lower().startswith("pokerbot") for part in path.parts)


def discover_files(client: LogStorageClient, workers: int) -> list[RemoteFile]:
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
                        # Root entries are hosts. Their direct children must be pokerbot
                        # services; this avoids recursively crawling unrelated services.
                        if depth == 1 and not name.lower().startswith("pokerbot"):
                            continue
                        next_directories.append((child, depth + 1))
                    elif entry_type == "file" and is_pokerbot_path(child):
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
) -> tuple[str, int]:
    destination = output.joinpath(*item.path.parts)
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_name(destination.name + ".part")
    if temporary.exists() and temporary.stat().st_size > item.size:
        temporary.unlink()
    if temporary.exists() and temporary.stat().st_size == item.size:
        os.replace(temporary, destination)
        set_mtime(destination, item.mtime)
        return str(item.path), item.size

    command = [
        "curl",
        "-fL",
        "--silent",
        "--show-error",
        "--retry",
        str(retries),
        "--retry-all-errors",
        "--connect-timeout",
        str(max(1, int(timeout))),
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
    return str(item.path), item.size


def parse_args() -> argparse.Namespace:
    script_dir = Path(__file__).resolve().parent
    default_output = script_dir.parent / "build" / "benapi"
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL)
    parser.add_argument("--output", type=Path, default=default_output)
    parser.add_argument("--workers", type=int, default=16)
    parser.add_argument("--timeout", type=float, default=30)
    parser.add_argument("--retries", type=int, default=5)
    parser.add_argument("--reserve-gib", type=float, default=2.0)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument(
        "--ignore-space-check",
        action="store_true",
        help="download even when free-space preflight fails",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.workers < 1 or args.retries < 1 or args.reserve_gib < 0:
        raise SystemExit("workers/retries must be positive and reserve-gib must be non-negative")
    if shutil.which("curl") is None:
        raise SystemExit("curl is required")

    output = args.output.expanduser().resolve()
    output.mkdir(parents=True, exist_ok=True)
    client = LogStorageClient(args.base_url, args.timeout, args.retries)
    print(f"Discovering pokerbot logs from {client.base_url} ...", flush=True)
    files = discover_files(client, args.workers)
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

    lock = threading.Lock()
    completed_files = 0
    completed_bytes = 0
    failures: list[tuple[str, str]] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as executor:
        futures = {
            executor.submit(download_one, client, item, output, args.timeout, args.retries): item
            for item in pending
        }
        for future in concurrent.futures.as_completed(futures):
            item = futures[future]
            try:
                path, size = future.result()
                with lock:
                    completed_files += 1
                    completed_bytes += size
                    print(
                        f"[{completed_files}/{len(pending)}] {path} ({format_bytes(size)})",
                        flush=True,
                    )
            except Exception as error:  # keep downloading independent files
                failures.append((str(item.path), str(error)))
                print(f"FAILED {item.path}: {error}", file=sys.stderr, flush=True)

    print(
        f"Downloaded {completed_files} files ({format_bytes(completed_bytes)}); "
        f"failed {len(failures)}.",
        flush=True,
    )
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
