import io
import json
import logging
import tempfile
import unittest
from pathlib import Path
from typing import Any
from unittest.mock import patch

import requests
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from requests import Response

from myobj_sdk import MyObjAPIError, MyObjClient, MyObjHTTPError


def make_public_key() -> str:
    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    return (
        private_key.public_key()
        .public_bytes(
            serialization.Encoding.PEM,
            serialization.PublicFormat.SubjectPublicKeyInfo,
        )
        .decode("utf-8")
    )


def json_response(payload: dict[str, Any], status_code: int = 200) -> Response:
    response = Response()
    response.status_code = status_code
    response.headers["Content-Type"] = "application/json; charset=utf-8"
    response._content = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    return response


def binary_response(content: bytes) -> Response:
    response = Response()
    response.status_code = 200
    response.headers["Content-Type"] = "application/octet-stream"
    response.headers["Content-Length"] = str(len(content))
    response._content = content
    response._content_consumed = True
    return response


class FakeSession:
    def __init__(self, responses: list[Response]) -> None:
        self.responses = responses
        self.calls: list[dict[str, Any]] = []

    def request(self, method: str, url: str, **kwargs: Any) -> Response:
        self.calls.append({"method": method, "url": url, **kwargs})
        return self.responses.pop(0)


class FailingSession:
    def request(self, method: str, url: str, **kwargs: Any) -> Response:
        raise requests.ConnectionError("不能写入日志的网络异常详情")


class RecordingProgressBar:
    def __init__(self, **kwargs: Any) -> None:
        self.options = kwargs
        self.updates: list[int] = []
        self.refresh_count = 0
        self.closed = False

    def __enter__(self) -> "RecordingProgressBar":
        return self

    def __exit__(self, exc_type: Any, exc_value: Any, traceback: Any) -> None:
        self.closed = True

    def update(self, amount: int) -> None:
        self.updates.append(amount)

    def refresh(self) -> None:
        self.refresh_count += 1


class MyObjClientTest(unittest.TestCase):
    def make_client(self, session: FakeSession) -> MyObjClient:
        return MyObjClient(
            "http://localhost:8080",
            "test-key",
            public_key=make_public_key(),
            session=session,  # type: ignore[arg-type]
        )

    def test_list_files_sends_all_auth_headers(self) -> None:
        session = FakeSession(
            [json_response({"code": 200, "message": "ok", "data": []})]
        )
        client = self.make_client(session)

        client.list_files(page=2, page_size=10)

        call = session.calls[0]
        self.assertEqual(call["url"], "http://localhost:8080/api/file/list")
        self.assertEqual(call["params"]["page"], 2)
        self.assertEqual(call["params"]["pageSize"], 10)
        for name in ("X-API-Key", "X-Signature", "X-Timestamp", "X-Nonce"):
            self.assertIn(name, call["headers"])
        self.assertNotIn("Authorization", call["headers"])

    def test_search_files_accepts_tag_filter_without_keyword(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {"code": 200, "message": "ok", "data": {"files": [], "total": 0}}
                )
            ]
        )
        client = self.make_client(session)

        client.search_files(tag_ids=["tag-1", "tag-2", "tag-1"], tag_mode="any")

        params = session.calls[0]["params"]
        self.assertEqual(params["tag_ids"], "tag-1,tag-2")
        self.assertEqual(params["tag_mode"], "any")
        self.assertNotIn("keyword", params)

    def test_search_files_sends_directory_scope(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {"code": 200, "message": "ok", "data": {"files": [], "total": 0}}
                )
            ]
        )
        client = self.make_client(session)

        client.search_files("报告", directory_id=12)

        self.assertEqual(session.calls[0]["params"]["directory_id"], 12)

    def test_search_files_rejects_unbounded_search(self) -> None:
        client = self.make_client(FakeSession([]))

        with self.assertRaises(ValueError):
            client.search_files()

    def test_tag_and_dictionary_methods_serialize_utf8_content(self) -> None:
        session = FakeSession(
            [
                json_response({"code": 200, "message": "ok", "data": {}}),
                json_response({"code": 200, "message": "ok", "data": {}}),
            ]
        )
        client = self.make_client(session)

        client.update_manual_tags(
            "uf-1",
            add=[{"name": "流浪地球", "category_id": "title", "visibility": "private"}],
        )
        client.update_tag_dictionary(
            [
                {
                    "type": "word",
                    "pattern": "流浪地球",
                    "category_id": "title",
                    "enabled": True,
                }
            ]
        )

        self.assertEqual(session.calls[0]["method"], "PUT")
        self.assertEqual(session.calls[0]["json"]["add"][0]["name"], "流浪地球")
        self.assertEqual(
            session.calls[1]["url"], "http://localhost:8080/api/file/tag-dictionary"
        )
        self.assertEqual(session.calls[1]["json"]["rules"][0]["pattern"], "流浪地球")

    def test_tag_suggestions_and_single_file_retry(self) -> None:
        session = FakeSession(
            [
                json_response({"code": 200, "message": "ok", "data": []}),
                json_response({"code": 200, "message": "ok", "data": None}),
            ]
        )
        client = self.make_client(session)

        client.get_tag_suggestions("科幻", limit=10)
        client.retry_file_tags("uf-1")

        self.assertEqual(session.calls[0]["params"], {"keyword": "科幻", "limit": 10})
        self.assertEqual(session.calls[1]["method"], "POST")
        self.assertEqual(
            session.calls[1]["url"], "http://localhost:8080/api/file/tags/uf-1/retry"
        )

    def test_business_error_raises_api_error(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {"code": 401, "message": "API Key认证参数不完整", "data": None}
                )
            ]
        )
        client = self.make_client(session)

        with self.assertRaises(MyObjAPIError) as caught:
            client.get_directories()

        self.assertEqual(caught.exception.code, 401)

    def test_upload_uses_precheck_and_all_chunks(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {"code": 201, "message": "继续上传", "data": "precheck-1"}
                ),
                json_response({"code": 200, "message": "分片成功", "data": None}),
                json_response(
                    {"code": 200, "message": "上传成功", "data": {"id": "file-1"}}
                ),
            ]
        )
        client = self.make_client(session)

        with tempfile.TemporaryDirectory() as temp_dir:
            file_path = Path(temp_dir) / "示例.txt"
            file_path.write_bytes(b"a" * MyObjClient.DEFAULT_CHUNK_SIZE + b"b")
            result = client.upload_file(
                file_path,
                1,
                show_progress=False,
            )

        self.assertEqual(result["data"]["id"], "file-1")
        self.assertEqual(len(session.calls), 3)
        self.assertEqual(session.calls[0]["json"]["files_md5"].__len__(), 2)
        self.assertEqual(session.calls[1]["data"]["chunk_index"], "0")
        self.assertEqual(session.calls[2]["data"]["chunk_index"], "1")
        self.assertEqual(session.calls[1]["timeout"], 30.0)
        self.assertEqual(session.calls[2]["timeout"], 30.0)
        self.assertEqual(session.calls[2]["data"]["async_finalize"], "true")

    def test_update_thumbnail_uploads_jpeg(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {
                        "code": 200,
                        "message": "修改缩略图成功",
                        "data": {"file_id": "user-file-1", "has_thumbnail": True},
                    }
                )
            ]
        )
        client = self.make_client(session)

        with tempfile.TemporaryDirectory() as temp_dir:
            thumbnail_path = Path(temp_dir) / "视频封面.jpg"
            thumbnail_path.write_bytes(b"jpeg-content")
            result = client.update_thumbnail("user-file-1", thumbnail_path)

        self.assertTrue(result["data"]["has_thumbnail"])
        call = session.calls[0]
        self.assertEqual(call["method"], "PUT")
        self.assertEqual(
            call["url"],
            "http://localhost:8080/api/file/thumbnail/user-file-1",
        )
        self.assertEqual(call["files"]["thumbnail"][0], "视频封面.jpg")
        self.assertEqual(call["files"]["thumbnail"][2], "image/jpeg")
        self.assertTrue(call["files"]["thumbnail"][1].closed)

    def test_update_thumbnail_rejects_missing_file(self) -> None:
        client = self.make_client(FakeSession([]))

        with self.assertRaises(FileNotFoundError):
            client.update_thumbnail("user-file-1", "不存在的缩略图.jpg")

    def test_resume_upload_uses_normal_timeout_for_last_pending_chunk(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            file_path = Path(temp_dir) / "断点续传.txt"
            file_path.write_bytes(b"a" * MyObjClient.DEFAULT_CHUNK_SIZE + b"b")
            _, chunk_md5s = MyObjClient._hash_file(
                file_path, MyObjClient.DEFAULT_CHUNK_SIZE
            )

            session = FakeSession(
                [
                    json_response(
                        {
                            "code": 201,
                            "message": "继续上传",
                            "data": {
                                "precheck_id": "precheck-1",
                                "md5": [chunk_md5s[1]],
                            },
                        }
                    ),
                    json_response(
                        {"code": 200, "message": "上传成功", "data": {"id": "file-1"}}
                    ),
                ]
            )
            client = self.make_client(session)

            client.upload_file(
                file_path,
                1,
                show_progress=False,
            )

        self.assertEqual(session.calls[1]["data"]["chunk_index"], "0")
        self.assertEqual(session.calls[1]["timeout"], 30.0)

    def test_upload_polls_until_background_processing_completes(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {"code": 201, "message": "继续上传", "data": "precheck-1"}
                ),
                json_response(
                    {
                        "code": 200,
                        "message": "文件正在后台处理",
                        "data": {"task_id": "precheck-1", "status": "processing"},
                    }
                ),
                json_response(
                    {
                        "code": 200,
                        "message": "查询成功",
                        "data": {"status": "processing", "stage": "storing"},
                    }
                ),
                json_response(
                    {
                        "code": 200,
                        "message": "查询成功",
                        "data": {"status": "completed", "file_id": "file-1"},
                    }
                ),
            ]
        )
        client = self.make_client(session)

        with tempfile.TemporaryDirectory() as temp_dir, patch("time.sleep") as sleep:
            file_path = Path(temp_dir) / "异步.txt"
            file_path.write_text("内容", encoding="utf-8")
            result = client.upload_file(
                file_path,
                1,
                show_progress=False,
                wait_for_completion=True,
            )

        self.assertEqual(result["data"]["id"], "file-1")
        self.assertEqual(session.calls[2]["params"]["precheck_id"], "precheck-1")
        self.assertEqual(session.calls[3]["params"]["precheck_id"], "precheck-1")
        sleep.assert_called_once_with(1.0)

    def test_download_file_polls_task_status_before_requesting_binary(self) -> None:
        content = "文件内容".encode("utf-8")
        session = FakeSession(
            [
                json_response(
                    {"code": 200, "message": "创建成功", "data": {"task_id": "task-1"}}
                ),
                json_response(
                    {"code": 200, "message": "查询成功", "data": {"state": 1}}
                ),
                json_response(
                    {"code": 200, "message": "查询成功", "data": {"state": 3}}
                ),
                binary_response(content),
            ]
        )
        client = self.make_client(session)
        with tempfile.TemporaryDirectory() as temp_dir, patch("time.sleep") as sleep:
            destination = Path(temp_dir) / "下载.bin"
            result = client.download_file("file-1", destination)
            self.assertEqual(result.read_bytes(), content)

        self.assertTrue(session.calls[1]["url"].endswith("/download/local/task/task-1"))
        self.assertTrue(session.calls[2]["url"].endswith("/download/local/task/task-1"))
        self.assertTrue(session.calls[3]["url"].endswith("/download/local/file/task-1"))
        sleep.assert_called_once_with(0.5)

    def test_download_polling_uses_backoff_and_max_interval(self) -> None:
        states = [1, 1, 1, 1, 1, 1, 3]
        session = FakeSession(
            [
                json_response(
                    {
                        "code": 200,
                        "message": "创建成功",
                        "data": {"task_id": "task-backoff"},
                    }
                ),
                *[
                    json_response(
                        {"code": 200, "message": "查询成功", "data": {"state": state}}
                    )
                    for state in states
                ],
                binary_response(b"done"),
            ]
        )
        client = self.make_client(session)
        with (
            tempfile.TemporaryDirectory() as temp_dir,
            patch("time.sleep") as sleep,
            patch("time.monotonic", return_value=0.0),
        ):
            client.download_file(
                "file-1",
                Path(temp_dir) / "退避.bin",
                poll_interval=0.5,
                max_poll_interval=2.0,
            )
        self.assertEqual(
            [call.args[0] for call in sleep.call_args_list],
            [0.5, 0.75, 1.125, 1.6875, 2.0, 2.0],
        )

    def test_download_polling_reports_failed_and_canceled_states(self) -> None:
        for state, message in ((4, "远端下载失败"), (5, "下载任务已取消")):
            with self.subTest(state=state):
                session = FakeSession(
                    [
                        json_response(
                            {
                                "code": 200,
                                "message": "创建成功",
                                "data": {"task_id": "task-error"},
                            }
                        ),
                        json_response(
                            {
                                "code": 200,
                                "message": "查询成功",
                                "data": {"state": state, "error_msg": "远端下载失败"},
                            }
                        ),
                    ]
                )
                client = self.make_client(session)
                with self.assertRaisesRegex(MyObjAPIError, message):
                    client.download_file("file-1", "不会创建.bin")
                self.assertEqual(len(session.calls), 2)

    def test_download_polling_honors_prepare_timeout(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {
                        "code": 200,
                        "message": "创建成功",
                        "data": {"task_id": "task-timeout"},
                    }
                ),
                json_response(
                    {"code": 200, "message": "查询成功", "data": {"state": 1}}
                ),
            ]
        )
        client = self.make_client(session)
        with patch("time.monotonic", side_effect=[0.0, 0.0, 0.0, 0.0, 0.0, 2.0]):
            with self.assertRaisesRegex(TimeoutError, "准备下载文件超时"):
                client.download_file("file-1", "不会创建.bin", prepare_timeout=1.0)

    def test_polling_parameters_are_validated_before_request(self) -> None:
        client = self.make_client(FakeSession([]))
        with self.assertRaisesRegex(ValueError, "poll_interval"):
            client.download_file("file-1", "目标.bin", poll_interval=0)
        with self.assertRaisesRegex(ValueError, "max_poll_interval"):
            client.download_package(
                "package-1",
                "目标.zip",
                poll_interval=2,
                max_poll_interval=1,
            )

    def test_upload_does_not_wait_for_background_processing_by_default(self) -> None:
        processing_response = {
            "code": 200,
            "message": "文件正在后台处理",
            "data": {
                "task_id": "precheck-1",
                "status": "processing",
                "is_complete": False,
            },
        }
        session = FakeSession(
            [
                json_response(
                    {"code": 201, "message": "继续上传", "data": "precheck-1"}
                ),
                json_response(processing_response),
            ]
        )
        client = self.make_client(session)

        with tempfile.TemporaryDirectory() as temp_dir:
            file_path = Path(temp_dir) / "异步返回.txt"
            file_path.write_text("内容", encoding="utf-8")
            result = client.upload_file(file_path, 1, show_progress=False)

        self.assertEqual(result, processing_response)
        self.assertEqual(len(session.calls), 2)

    def test_upload_rejects_removed_chunk_size_argument(self) -> None:
        client = self.make_client(FakeSession([]))

        with self.assertRaises(TypeError):
            client.upload_file("不存在.txt", 1, chunk_size=1024)  # type: ignore[call-arg]

    def test_ensure_directory_creates_missing_folder(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {
                        "code": 200,
                        "message": "ok",
                        "data": {"folders": [], "total": 0, "page_size": 100},
                    }
                ),
                json_response({"code": 200, "message": "创建目录成功", "data": None}),
                json_response(
                    {
                        "code": 200,
                        "message": "ok",
                        "data": {
                            "folders": [{"name": "演员甲", "id": 12}],
                            "total": 1,
                            "page_size": 100,
                        },
                    }
                ),
            ]
        )
        client = self.make_client(session)

        directory_id = client.ensure_directory(2, "演员甲")

        self.assertEqual(directory_id, 12)
        self.assertEqual(session.calls[1]["json"], {"parent_id": 2, "name": "演员甲"})

    def test_debug_logs_request_response_and_redacts_password(self) -> None:
        session = FakeSession(
            [json_response({"code": 200, "message": "任务已创建", "data": {}})]
        )
        client = self.make_client(session)

        with self.assertLogs("myobj_sdk", level="DEBUG") as captured:
            client.create_file_download("file-1", file_password="绝密密码")

        logs = "\n".join(captured.output)
        self.assertIn("请求开始 method=POST", logs)
        self.assertIn("请求完成 method=POST", logs)
        self.assertIn("status_code=200", logs)
        self.assertIn("业务响应 method=POST", logs)
        self.assertIn("code=200", logs)
        self.assertIn("message=任务已创建", logs)
        self.assertIn("<已脱敏>", logs)
        self.assertNotIn("绝密密码", logs)
        self.assertNotIn("test-key", logs)
        self.assertNotIn("X-Signature", logs)

    def test_debug_logs_summarize_upload_without_file_content(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {"code": 201, "message": "继续上传", "data": "precheck-1"}
                ),
                json_response(
                    {"code": 200, "message": "上传成功", "data": {"id": "1"}}
                ),
            ]
        )
        client = self.make_client(session)

        with tempfile.TemporaryDirectory() as temp_dir:
            file_path = Path(temp_dir) / "秘密.txt"
            file_path.write_text("不能写入日志的文件内容", encoding="utf-8")
            with self.assertLogs("myobj_sdk", level="DEBUG") as captured:
                client.upload_file(
                    file_path,
                    1,
                    encrypted=True,
                    file_password="上传密码",
                    show_progress=False,
                )

        logs = "\n".join(captured.output)
        self.assertIn("'files': {'file': {'文件名': '秘密.txt'", logs)
        self.assertIn("<已脱敏>", logs)
        self.assertNotIn("上传密码", logs)
        self.assertNotIn("不能写入日志的文件内容", logs)

    def test_network_error_log_omits_exception_detail(self) -> None:
        client = self.make_client(FailingSession())  # type: ignore[arg-type]

        with self.assertLogs("myobj_sdk", level="DEBUG") as captured:
            with self.assertRaises(MyObjHTTPError):
                client.get_directories()

        logs = "\n".join(captured.output)
        self.assertIn("请求失败 method=GET", logs)
        self.assertIn("error_type=ConnectionError", logs)
        self.assertNotIn("不能写入日志的网络异常详情", logs)

    def test_upload_default_progress_and_callback_advance_by_chunk(self) -> None:
        session = FakeSession(
            [
                json_response(
                    {"code": 201, "message": "继续上传", "data": "precheck-1"}
                ),
                json_response({"code": 200, "message": "分片成功", "data": None}),
                json_response({"code": 200, "message": "上传成功", "data": None}),
            ]
        )
        client = self.make_client(session)
        progress_bars: list[RecordingProgressBar] = []
        callback_values: list[tuple[int, int]] = []

        def make_progress_bar(**kwargs: Any) -> RecordingProgressBar:
            progress_bar = RecordingProgressBar(**kwargs)
            progress_bars.append(progress_bar)
            return progress_bar

        with tempfile.TemporaryDirectory() as temp_dir:
            file_path = Path(temp_dir) / "示例.txt"
            file_path.write_bytes(b"a" * MyObjClient.DEFAULT_CHUNK_SIZE + b"b")
            with (
                patch("myobj_sdk.client.tqdm", side_effect=make_progress_bar),
                patch("myobj_sdk.client.logging_redirect_tqdm") as redirect,
            ):
                redirect.return_value.__enter__.return_value = None
                client.upload_file(
                    file_path,
                    1,
                    progress=lambda completed, total: callback_values.append(
                        (completed, total)
                    ),
                )

        self.assertEqual(len(progress_bars), 1)
        expected_total = MyObjClient.DEFAULT_CHUNK_SIZE + 1
        self.assertEqual(progress_bars[0].options["total"], expected_total)
        self.assertEqual(progress_bars[0].updates, [MyObjClient.DEFAULT_CHUNK_SIZE, 1])
        self.assertEqual(progress_bars[0].refresh_count, 1)
        self.assertTrue(progress_bars[0].closed)
        self.assertEqual(
            callback_values,
            [
                (MyObjClient.DEFAULT_CHUNK_SIZE, expected_total),
                (expected_total, expected_total),
            ],
        )
        redirect.assert_called_once()

    def test_upload_can_disable_progress_bar(self) -> None:
        session = FakeSession(
            [json_response({"code": 200, "message": "秒传成功", "data": {}})]
        )
        client = self.make_client(session)

        with tempfile.TemporaryDirectory() as temp_dir:
            file_path = Path(temp_dir) / "示例.txt"
            file_path.write_text("abc", encoding="utf-8")
            with (
                patch("myobj_sdk.client.tqdm") as progress_bar,
                patch("myobj_sdk.client.logging_redirect_tqdm") as redirect,
            ):
                client.upload_file(file_path, 1, show_progress=False)

        progress_bar.assert_not_called()
        redirect.assert_not_called()

    def test_instant_upload_completes_zero_byte_progress(self) -> None:
        session = FakeSession(
            [json_response({"code": 200, "message": "秒传成功", "data": {}})]
        )
        client = self.make_client(session)
        progress_bars: list[RecordingProgressBar] = []
        callback_values: list[tuple[int, int]] = []

        def make_progress_bar(**kwargs: Any) -> RecordingProgressBar:
            progress_bar = RecordingProgressBar(**kwargs)
            progress_bars.append(progress_bar)
            return progress_bar

        with tempfile.TemporaryDirectory() as temp_dir:
            file_path = Path(temp_dir) / "空文件.txt"
            file_path.write_bytes(b"")
            with (
                patch("myobj_sdk.client.tqdm", side_effect=make_progress_bar),
                patch("myobj_sdk.client.logging_redirect_tqdm") as redirect,
            ):
                redirect.return_value.__enter__.return_value = None
                client.upload_file(
                    file_path,
                    1,
                    progress=lambda completed, total: callback_values.append(
                        (completed, total)
                    ),
                )

        self.assertEqual(progress_bars[0].options["total"], 0)
        self.assertEqual(progress_bars[0].updates, [0])
        self.assertEqual(progress_bars[0].refresh_count, 1)
        self.assertTrue(progress_bars[0].closed)
        self.assertEqual(callback_values, [(0, 0)])

    def test_upload_error_closes_progress_and_restores_logging_handler(self) -> None:
        class UploadFailingSession(FakeSession):
            def request(self, method: str, url: str, **kwargs: Any) -> Response:
                if self.responses:
                    return super().request(method, url, **kwargs)
                raise requests.ConnectionError("上传中断")

        session = UploadFailingSession(
            [json_response({"code": 201, "message": "继续上传", "data": "task"})]
        )
        client = self.make_client(session)
        progress_bars: list[RecordingProgressBar] = []

        def make_progress_bar(**kwargs: Any) -> RecordingProgressBar:
            progress_bar = RecordingProgressBar(**kwargs)
            progress_bars.append(progress_bar)
            return progress_bar

        root_logger = logging.getLogger()
        handler = logging.StreamHandler(io.StringIO())
        root_logger.addHandler(handler)
        original_handlers = list(root_logger.handlers)
        try:
            with tempfile.TemporaryDirectory() as temp_dir:
                file_path = Path(temp_dir) / "失败.txt"
                file_path.write_text("abc", encoding="utf-8")
                with patch("myobj_sdk.client.tqdm", side_effect=make_progress_bar):
                    with self.assertRaises(MyObjHTTPError):
                        client.upload_file(file_path, 1)

            self.assertEqual(root_logger.handlers, original_handlers)
            self.assertTrue(progress_bars[0].closed)
        finally:
            root_logger.removeHandler(handler)


if __name__ == "__main__":
    unittest.main()
