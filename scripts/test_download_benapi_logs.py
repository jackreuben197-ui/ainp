import sys
import tempfile
import unittest
from pathlib import Path, PurePosixPath
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import download_benapi_logs as downloader


class DownloadBenapiLogsTest(unittest.TestCase):
    def test_service_range_requires_exact_two_digit_ainp_name(self) -> None:
        matches = downloader.matches_service_name
        self.assertTrue(matches("ainp01", "ainp", 1, 20, 2))
        self.assertTrue(matches("AINP20", "ainp", 1, 20, 2))
        self.assertFalse(matches("ainp00", "ainp", 1, 20, 2))
        self.assertFalse(matches("ainp21", "ainp", 1, 20, 2))
        self.assertFalse(matches("ainp1", "ainp", 1, 20, 2))
        self.assertFalse(matches("ainp010", "ainp", 1, 20, 2))
        self.assertFalse(matches("pokerbot01", "ainp", 1, 20, 2))

    def test_ainp_entry_defaults(self) -> None:
        output = Path("/tmp/ainp-log-test")
        args = downloader.parse_args(
            [],
            default_output=output,
            default_service_prefix="ainp",
            default_service_min=1,
            default_service_max=20,
            default_service_digits=2,
        )
        self.assertEqual(output, args.output)
        self.assertEqual(("ainp", 1, 20, 2), (
            args.service_prefix,
            args.service_min,
            args.service_max,
            args.service_digits,
        ))

    def test_classify_skips_complete_and_counts_largest_reusable_prefix(self) -> None:
        files = [
            downloader.RemoteFile(PurePosixPath("host/pokerbot01/complete.log"), 10, ""),
            downloader.RemoteFile(PurePosixPath("host/pokerbot01/resume.log"), 20, ""),
        ]
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            complete = output / "host/pokerbot01/complete.log"
            resume = output / "host/pokerbot01/resume.log"
            complete.parent.mkdir(parents=True)
            complete.write_bytes(b"x" * 10)
            resume.write_bytes(b"x" * 7)
            resume.with_name("resume.log.part").write_bytes(b"x" * 12)

            pending, skipped_bytes, pending_bytes = downloader.classify(files, output)

        self.assertEqual([files[1]], pending)
        self.assertEqual(10, skipped_bytes)
        self.assertEqual(8, pending_bytes)

    def test_download_reuses_incomplete_destination(self) -> None:
        item = downloader.RemoteFile(PurePosixPath("host/pokerbot01/test.log"), 10, "")
        client = downloader.LogStorageClient("https://example.invalid", 30, 1)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            destination = output / "host/pokerbot01/test.log"
            destination.parent.mkdir(parents=True)
            destination.write_bytes(b"x" * 6)

            def finish_download(command: list[str], check: bool) -> None:
                self.assertTrue(check)
                temporary = Path(command[command.index("--output") + 1])
                self.assertEqual(6, temporary.stat().st_size)
                with temporary.open("ab") as stream:
                    stream.write(b"y" * 4)

            with mock.patch.object(downloader.subprocess, "run", side_effect=finish_download):
                path, size, transferred = downloader.download_one(
                    client, item, output, timeout=30, retries=1
                )

            self.assertEqual("host/pokerbot01/test.log", path)
            self.assertEqual(10, size)
            self.assertEqual(4, transferred)
            self.assertEqual(b"x" * 6 + b"y" * 4, destination.read_bytes())
            self.assertFalse(destination.with_name("test.log.part").exists())


if __name__ == "__main__":
    unittest.main()
