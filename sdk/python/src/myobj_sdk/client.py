"""MyObj 文件管理客户端。"""

from __future__ import annotations

import hashlib
import time
from contextlib import ExitStack
from pathlib import Path
from typing import Any, Callable, Iterable, Mapping, Optional, Sequence, Union

import requests
from requests import Response, Session

from .auth import ApiKeyAuth, PathLike
from .exceptions import MyObjAPIError, MyObjHTTPError


ProgressCallback = Callable[[int, int], None]
Timeout = Union[float, tuple[float, float]]


class MyObjClient:
    """使用 API Key 管理 MyObj 文件。

    ``base_url`` 可以填写 ``http://localhost:8080`` 或
    ``http://localhost:8080/api``。
    """

    DEFAULT_CHUNK_SIZE = 5 * 1024 * 1024
    DOWNLOAD_BLOCK_SIZE = 1024 * 1024

    def __init__(
        self,
        base_url: str,
        api_key: str,
        *,
        public_key: Optional[str] = None,
        public_key_path: Optional[PathLike] = None,
        timeout: Timeout = 30.0,
        verify: Union[bool, str] = True,
        session: Optional[Session] = None,
    ) -> None:
        if bool(public_key) == bool(public_key_path):
            raise ValueError("public_key 和 public_key_path 必须且只能提供一个")

        normalized_url = base_url.rstrip("/")
        self.api_url = (
            normalized_url
            if normalized_url.endswith("/api")
            else f"{normalized_url}/api"
        )
        self.timeout = timeout
        self.verify = verify
        self.session = session or requests.Session()
        self._owns_session = session is None
        self.auth = (
            ApiKeyAuth(api_key, public_key)
            if public_key is not None
            else ApiKeyAuth.from_file(api_key, public_key_path)  # type: ignore[arg-type]
        )

    def __enter__(self) -> "MyObjClient":
        return self

    def __exit__(self, exc_type: Any, exc_value: Any, traceback: Any) -> None:
        self.close()

    def close(self) -> None:
        """关闭客户端创建的 HTTP 会话。"""

        if self._owns_session:
            self.session.close()

    def _request(self, method: str, path: str, **kwargs: Any) -> Response:
        custom_headers = dict(kwargs.pop("headers", {}) or {})
        headers = {**custom_headers, **self.auth.build_headers()}
        try:
            return self.session.request(
                method,
                f"{self.api_url}{path}",
                headers=headers,
                timeout=kwargs.pop("timeout", self.timeout),
                verify=kwargs.pop("verify", self.verify),
                **kwargs,
            )
        except requests.RequestException as exc:
            raise MyObjHTTPError(f"请求 MyObj 失败: {exc}") from exc

    @staticmethod
    def _decode_json(response: Response) -> Mapping[str, Any]:
        try:
            payload = response.json()
        except ValueError as exc:
            raise MyObjHTTPError(
                "MyObj 返回了无法解析的 JSON",
                status_code=response.status_code,
            ) from exc
        if not isinstance(payload, Mapping):
            raise MyObjHTTPError(
                "MyObj 返回的 JSON 不是对象",
                status_code=response.status_code,
            )
        return payload

    def _request_json(
        self,
        method: str,
        path: str,
        *,
        accepted_codes: Sequence[int] = (200,),
        **kwargs: Any,
    ) -> dict[str, Any]:
        response = self._request(method, path, **kwargs)
        payload = dict(self._decode_json(response))
        code = int(payload.get("code", response.status_code))
        message = str(payload.get("message", "请求失败"))

        if response.status_code >= 400 and code in accepted_codes:
            raise MyObjHTTPError(message, status_code=response.status_code)
        if code not in accepted_codes:
            raise MyObjAPIError(
                code,
                message,
                data=payload.get("data"),
                status_code=response.status_code,
            )
        return payload

    def _request_binary(self, method: str, path: str, **kwargs: Any) -> Response:
        response = self._request(method, path, stream=True, **kwargs)
        content_type = response.headers.get("Content-Type", "").lower()
        if "json" in content_type:
            payload = dict(self._decode_json(response))
            raise MyObjAPIError(
                int(payload.get("code", response.status_code)),
                str(payload.get("message", "下载失败")),
                data=payload.get("data"),
                status_code=response.status_code,
            )
        if response.status_code >= 400:
            raise MyObjHTTPError("下载请求失败", status_code=response.status_code)
        return response

    @staticmethod
    def _params(**values: Any) -> dict[str, Any]:
        return {key: value for key, value in values.items() if value not in (None, "")}

    @staticmethod
    def _check_page(page: int, page_size: int) -> None:
        if page < 1:
            raise ValueError("page 必须大于等于 1")
        if not 1 <= page_size <= 100:
            raise ValueError("page_size 必须在 1 到 100 之间")

    # 文件与目录

    def list_files(
        self,
        *,
        virtual_path: str = "",
        file_type: str = "",
        sort_by: str = "",
        page: int = 1,
        page_size: int = 20,
    ) -> dict[str, Any]:
        """获取当前用户指定目录下的文件列表。"""

        self._check_page(page, page_size)
        return self._request_json(
            "GET",
            "/file/list",
            params=self._params(
                virtualPath=virtual_path,
                type=file_type,
                sortBy=sort_by,
                page=page,
                pageSize=page_size,
            ),
        )

    def search_files(
        self,
        keyword: str,
        *,
        file_type: str = "",
        sort_by: str = "",
        page: int = 1,
        page_size: int = 20,
    ) -> dict[str, Any]:
        """搜索当前用户的文件。"""

        self._check_page(page, page_size)
        if not keyword:
            raise ValueError("keyword 不能为空")
        return self._request_json(
            "GET",
            "/file/search/user",
            params=self._params(
                keyword=keyword,
                type=file_type,
                sortBy=sort_by,
                page=page,
                pageSize=page_size,
            ),
        )

    def search_public_files(
        self,
        keyword: str,
        *,
        file_type: str = "",
        sort_by: str = "",
        page: int = 1,
        page_size: int = 20,
    ) -> dict[str, Any]:
        """搜索公开文件。当前服务端仍要求认证。"""

        self._check_page(page, page_size)
        if not keyword:
            raise ValueError("keyword 不能为空")
        return self._request_json(
            "GET",
            "/file/search/public",
            params=self._params(
                keyword=keyword,
                type=file_type,
                sortBy=sort_by,
                page=page,
                pageSize=page_size,
            ),
        )

    def list_public_files(
        self,
        *,
        file_type: str = "",
        sort_by: str = "",
        page: int = 1,
        page_size: int = 20,
    ) -> dict[str, Any]:
        """获取文件广场列表。"""

        self._check_page(page, page_size)
        return self._request_json(
            "GET",
            "/file/public/list",
            params=self._params(
                type=file_type,
                sortBy=sort_by,
                page=page,
                pageSize=page_size,
            ),
        )

    def get_virtual_paths(self) -> dict[str, Any]:
        """获取当前用户的虚拟目录树。"""

        return self._request_json("GET", "/file/virtualPath")

    def create_directory(self, parent_level: str, dir_path: str) -> dict[str, Any]:
        """创建目录。"""

        return self._request_json(
            "POST",
            "/file/makeDir",
            json={"parent_level": parent_level, "dir_path": dir_path},
        )

    def ensure_directory(
        self,
        parent_level: str,
        dir_path: str,
        *,
        page_size: int = 100,
    ) -> str:
        """查找目录，不存在时创建，并返回目录的路径 ID。"""

        if not dir_path.strip():
            raise ValueError("dir_path 不能为空")
        self._check_page(1, page_size)

        def find_directory() -> Optional[str]:
            page = 1
            while True:
                result = self.list_files(
                    virtual_path=str(parent_level),
                    page=page,
                    page_size=page_size,
                )
                data = result.get("data")
                if not isinstance(data, Mapping):
                    raise MyObjHTTPError("文件列表响应中缺少 data 对象")

                folders = data.get("folders")
                if isinstance(folders, list):
                    for folder in folders:
                        if not isinstance(folder, Mapping):
                            continue
                        if (
                            folder.get("name") == dir_path
                            and folder.get("path") is not None
                        ):
                            return str(folder["path"])

                total = int(data.get("total", 0) or 0)
                actual_page_size = int(data.get("page_size", page_size) or page_size)
                if page * actual_page_size >= total:
                    return None
                page += 1

        existing_path = find_directory()
        if existing_path is not None:
            return existing_path

        self.create_directory(str(parent_level), dir_path)
        created_path = find_directory()
        if created_path is None:
            raise MyObjHTTPError(f"目录 {dir_path} 创建成功，但未能查询到路径 ID")
        return created_path

    def move_file(
        self, file_id: str, source_path: str, target_path: str
    ) -> dict[str, Any]:
        """移动文件。"""

        return self._request_json(
            "POST",
            "/file/move",
            json={
                "file_id": file_id,
                "source_path": source_path,
                "target_path": target_path,
            },
        )

    def delete_files(self, file_ids: Iterable[str]) -> dict[str, Any]:
        """将文件移动到回收站。"""

        ids = list(file_ids)
        if not ids:
            raise ValueError("file_ids 不能为空")
        return self._request_json("POST", "/file/delete", json={"file_ids": ids})

    def rename_file(self, file_id: str, new_file_name: str) -> dict[str, Any]:
        """重命名文件。"""

        return self._request_json(
            "POST",
            "/file/rename",
            json={"file_id": file_id, "new_file_name": new_file_name},
        )

    def rename_directory(self, dir_id: int, new_dir_name: str) -> dict[str, Any]:
        """重命名目录。"""

        return self._request_json(
            "POST",
            "/file/renameDir",
            json={"dir_id": dir_id, "new_dir_name": new_dir_name},
        )

    def delete_directory(self, dir_id: int) -> dict[str, Any]:
        """删除目录及其中内容。"""

        return self._request_json("POST", "/file/deleteDir", json={"dir_id": dir_id})

    def set_file_public(self, file_id: str, public: bool) -> dict[str, Any]:
        """设置文件公开状态。"""

        return self._request_json(
            "POST",
            "/file/setPublic",
            json={"file_id": file_id, "public": public},
        )

    # 上传

    @staticmethod
    def _new_md5() -> Any:
        # MD5 是 MyObj 上传协议的一部分，此处不用于安全校验。
        try:
            return hashlib.md5(usedforsecurity=False)
        except TypeError:
            return hashlib.md5()

    @classmethod
    def _hash_file(cls, file_path: Path, chunk_size: int) -> tuple[str, list[str]]:
        whole_hash = cls._new_md5()
        chunk_hashes: list[str] = []
        with file_path.open("rb") as source:
            while True:
                chunk = source.read(chunk_size)
                if not chunk:
                    break
                whole_hash.update(chunk)
                chunk_hash = cls._new_md5()
                chunk_hash.update(chunk)
                chunk_hashes.append(chunk_hash.hexdigest())

        if not chunk_hashes:
            chunk_hashes.append(cls._new_md5().hexdigest())
        return whole_hash.hexdigest(), chunk_hashes

    def upload_file(
        self,
        file_path: PathLike,
        path_id: str,
        *,
        chunk_size: int = DEFAULT_CHUNK_SIZE,
        encrypted: bool = False,
        file_password: str = "",
        thumbnail_path: Optional[PathLike] = None,
        progress: Optional[ProgressCallback] = None,
    ) -> dict[str, Any]:
        """预检并上传文件，支持秒传、分片上传和断点续传。"""

        source_path = Path(file_path)
        if not source_path.is_file():
            raise FileNotFoundError(f"文件不存在: {source_path}")
        if chunk_size <= 0:
            raise ValueError("chunk_size 必须大于 0")
        if encrypted and not file_password:
            raise ValueError("加密上传必须提供 file_password")

        file_size = source_path.stat().st_size
        file_md5, chunk_md5s = self._hash_file(source_path, chunk_size)
        precheck = self._request_json(
            "POST",
            "/file/upload/precheck",
            accepted_codes=(200, 201),
            json={
                "chunk_signature": file_md5,
                "file_name": source_path.name,
                "file_size": file_size,
                "files_md5": chunk_md5s,
                "path_id": path_id,
            },
        )

        if int(precheck["code"]) == 200:
            if progress:
                progress(file_size, file_size)
            return precheck

        precheck_data = precheck.get("data")
        if isinstance(precheck_data, str):
            precheck_id = precheck_data
            uploaded_md5s: set[str] = set()
        elif isinstance(precheck_data, Mapping):
            precheck_id = str(
                precheck_data.get("precheck_id") or precheck_data.get("id") or ""
            )
            existing_md5s = precheck_data.get("md5")
            uploaded_md5s = set(
                existing_md5s if isinstance(existing_md5s, list) else []
            )
        else:
            precheck_id = ""
            uploaded_md5s = set()
        if not precheck_id:
            raise MyObjHTTPError("预检成功，但响应中没有 precheck_id")

        total_chunks = len(chunk_md5s)
        pending_chunks = sum(
            chunk_md5 not in uploaded_md5s for chunk_md5 in chunk_md5s
        )
        uploaded_bytes = 0
        last_response = precheck
        thumbnail_sent = False

        with source_path.open("rb") as source:
            for chunk_index, chunk_md5 in enumerate(chunk_md5s):
                chunk = source.read(chunk_size)
                if chunk_md5 in uploaded_md5s:
                    uploaded_bytes += len(chunk)
                    if progress:
                        progress(uploaded_bytes, file_size)
                    continue

                form = {
                    "precheck_id": precheck_id,
                    "chunk_index": str(chunk_index),
                    "total_chunks": str(total_chunks),
                    "chunk_md5": chunk_md5,
                    "is_enc": "true" if encrypted else "false",
                    "file_password": file_password if encrypted else "",
                }
                files: dict[str, Any] = {
                    "file": (source_path.name, chunk, "application/octet-stream")
                }

                with ExitStack() as stack:
                    if thumbnail_path is not None and not thumbnail_sent:
                        thumbnail = Path(thumbnail_path)
                        thumbnail_stream = stack.enter_context(thumbnail.open("rb"))
                        files["thumbnail"] = (
                            thumbnail.name,
                            thumbnail_stream,
                            "image/jpeg",
                        )
                        thumbnail_sent = True
                    last_response = self._request_json(
                        "POST",
                        "/file/upload",
                        data=form,
                        files=files,
                        # 补齐最后一个待上传分片后，服务端会同步合并并处理文件，
                        # 该过程耗时取决于文件大小，因此不设置读取超时。
                        timeout=None if pending_chunks == 1 else self.timeout,
                    )
                    pending_chunks -= 1

                uploaded_bytes += len(chunk)
                if progress:
                    progress(uploaded_bytes, file_size)

        return last_response

    def get_upload_progress(self, precheck_id: str) -> dict[str, Any]:
        return self._request_json(
            "GET",
            "/file/upload/progress",
            params={"precheck_id": precheck_id},
        )

    def list_upload_tasks(
        self, *, page: int = 1, page_size: int = 20
    ) -> dict[str, Any]:
        self._check_page(page, page_size)
        return self._request_json(
            "GET",
            "/file/upload/taskList",
            params={"page": page, "pageSize": page_size},
        )

    def list_uncompleted_uploads(self) -> dict[str, Any]:
        return self._request_json("GET", "/file/upload/uncompleted")

    def list_expired_uploads(self) -> dict[str, Any]:
        return self._request_json("GET", "/file/upload/expired")

    def delete_upload_task(self, task_id: str) -> dict[str, Any]:
        return self._request_json(
            "POST", "/file/upload/delete", json={"task_id": task_id}
        )

    def renew_upload_task(self, task_id: str, *, days: int = 7) -> dict[str, Any]:
        return self._request_json(
            "POST",
            "/file/upload/renew",
            json={"task_id": task_id, "days": days},
        )

    def clean_expired_uploads(self) -> dict[str, Any]:
        return self._request_json("POST", "/file/upload/clean-expired")

    # 下载与缩略图

    @classmethod
    def _save_binary_response(
        cls,
        response: Response,
        destination: PathLike,
        *,
        progress: Optional[ProgressCallback] = None,
    ) -> Path:
        target = Path(destination)
        target.parent.mkdir(parents=True, exist_ok=True)
        partial = target.with_name(f"{target.name}.part")
        total = int(response.headers.get("Content-Length", "0") or 0)
        written = 0
        with partial.open("wb") as output:
            for block in response.iter_content(chunk_size=cls.DOWNLOAD_BLOCK_SIZE):
                if not block:
                    continue
                output.write(block)
                written += len(block)
                if progress:
                    progress(written, total)
        partial.replace(target)
        return target

    def download_thumbnail(self, file_id: str, destination: PathLike) -> Path:
        """下载文件缩略图。"""

        response = self._request_binary("GET", f"/file/thumbnail/{file_id}")
        return self._save_binary_response(response, destination)

    def create_file_download(
        self, file_id: str, *, file_password: str = ""
    ) -> dict[str, Any]:
        """创建网盘文件下载准备任务。"""

        return self._request_json(
            "POST",
            "/download/local/create",
            json={"file_id": file_id, "file_password": file_password},
        )

    def download_file(
        self,
        file_id: str,
        destination: PathLike,
        *,
        file_password: str = "",
        prepare_timeout: float = 300.0,
        poll_interval: float = 0.5,
        progress: Optional[ProgressCallback] = None,
    ) -> Path:
        """等待服务端完成文件准备后，将文件流式保存到指定路径。"""

        task = self.create_file_download(file_id, file_password=file_password)
        data = task.get("data")
        if not isinstance(data, Mapping) or not data.get("task_id"):
            raise MyObjHTTPError("创建下载任务成功，但响应中没有 task_id")
        task_id = str(data["task_id"])

        deadline = time.monotonic() + prepare_timeout
        while True:
            try:
                response = self._request_binary(
                    "GET", f"/download/local/file/{task_id}"
                )
                break
            except MyObjAPIError as exc:
                if exc.code != 400 or "未准备完成" not in exc.message:
                    raise
                if time.monotonic() >= deadline:
                    raise TimeoutError("等待 MyObj 准备下载文件超时") from exc
                time.sleep(poll_interval)

        return self._save_binary_response(response, destination, progress=progress)

    # 打包下载

    def create_package(
        self,
        file_ids: Iterable[str],
        *,
        package_name: str = "",
    ) -> dict[str, Any]:
        ids = list(file_ids)
        if not ids:
            raise ValueError("file_ids 不能为空")
        return self._request_json(
            "POST",
            "/file/package/create",
            json={"file_ids": ids, "package_name": package_name},
        )

    def get_package_progress(self, package_id: str) -> dict[str, Any]:
        return self._request_json(
            "GET",
            "/file/package/progress",
            params={"package_id": package_id},
        )

    def download_package(
        self,
        package_id: str,
        destination: PathLike,
        *,
        wait_timeout: float = 300.0,
        poll_interval: float = 0.5,
        progress: Optional[ProgressCallback] = None,
    ) -> Path:
        deadline = time.monotonic() + wait_timeout
        while True:
            state = self.get_package_progress(package_id)
            data = state.get("data")
            status = data.get("status") if isinstance(data, Mapping) else None
            if status == "ready":
                break
            if status == "failed":
                message = str(data.get("error_msg") or "打包失败")
                raise MyObjAPIError(500, message, data=data)
            if time.monotonic() >= deadline:
                raise TimeoutError("等待 MyObj 打包文件超时")
            time.sleep(poll_interval)

        response = self._request_binary(
            "GET",
            "/file/package/download",
            params={"package_id": package_id},
        )
        return self._save_binary_response(response, destination, progress=progress)
