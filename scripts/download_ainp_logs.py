#!/usr/bin/env python3
"""Incrementally download ainp01 through ainp20 logs from logstorage."""

from pathlib import Path

from download_benapi_logs import main


if __name__ == "__main__":
    script_dir = Path(__file__).resolve().parent
    raise SystemExit(
        main(
            default_output=script_dir.parent / "build" / "ainp_log",
            default_service_prefix="ainp",
            default_service_min=1,
            default_service_max=20,
            default_service_digits=2,
            description=__doc__,
        )
    )
