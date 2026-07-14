import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from requests import Response

from myobj_sdk import MyObjAPIError, MyObjClient


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


class FakeSession:
    def __init__(self, responses: list[Response]) -> None:
        self.responses = responses
        self.calls: list[dict[str, Any]] = []

    def request(self, method: str, url: str, **kwargs: Any) -> Response:
        self.calls.append({"method": method, "url": url, **kwargs})
        return self.responses.pop(0)


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
            client.get_virtual_paths()

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
            file_path.write_text("abcdef", encoding="utf-8")
            result = client.upload_file(file_path, "root", chunk_size=3)

        self.assertEqual(result["data"]["id"], "file-1")
        self.assertEqual(len(session.calls), 3)
        self.assertEqual(session.calls[0]["json"]["files_md5"].__len__(), 2)
        self.assertEqual(session.calls[1]["data"]["chunk_index"], "0")
        self.assertEqual(session.calls[2]["data"]["chunk_index"], "1")

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
                            "folders": [{"name": "演员甲", "path": "12"}],
                            "total": 1,
                            "page_size": 100,
                        },
                    }
                ),
            ]
        )
        client = self.make_client(session)

        path_id = client.ensure_directory("2", "演员甲")

        self.assertEqual(path_id, "12")
        self.assertEqual(
            session.calls[1]["json"], {"parent_level": "2", "dir_path": "演员甲"}
        )


if __name__ == "__main__":
    unittest.main()
