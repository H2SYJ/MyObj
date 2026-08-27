"""MyObj 文件管理客户端。"""

from __future__ import annotations

import hashlib
import logging
import time
from contextlib import ExitStack
from pathlib import Path
from typing import Any, Callable, Iterable, Mapping, Optional, Sequence, Union

import requests
from requests import Response, Session
from tqdm import tqdm
from tqdm.contrib.logging import logging_redirect_tqdm

from .auth import ApiKeyAuth, PathLike
from .exceptions import MyObjAPIError, MyObjHTTPError


ProgressCallback = Callable[[int, int], None]
Timeout = Union[float, tuple[float, float]]
LOGGER = logging.getLogger("myobj_sdk")
REDACTED = "<已脱敏>"
SENSITIVE_LOG_KEYWORDS = (
    "api_key",
    "apikey",
    "authorization",
    "cookie",
    "nonce",
    "password",
    "signature",
    "token",
)


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

    @staticmethod
    def _is_sensitive_log_key(key: Any) -> bool:
        normalized_key = str(key).lower().replace("-", "_")
        return any(keyword in normalized_key for keyword in SENSITIVE_LOG_KEYWORDS)

    @classmethod
    def _sanitize_log_value(cls, value: Any, *, key: Any = "") -> Any:
        if cls._is_sensitive_log_key(key):
            return REDACTED
        if isinstance(value, Mapping):
            return {
                item_key: cls._sanitize_log_value(item_value, key=item_key)
                for item_key, item_value in value.items()
            }
        if isinstance(value, (list, tuple)):
            return [cls._sanitize_log_value(item) for item in value]
        if isinstance(value, (bytes, bytearray, memoryview)):
            return f"<{type(value).__name__}，{len(value)} 字节>"
        if hasattr(value, "read"):
            name = getattr(value, "name", None)
            return f"<文件流 {Path(str(name)).name if name else '未命名'}>"
        return value

    @classmethod
    def _summarize_log_files(cls, files: Any) -> Any:
        if not isinstance(files, Mapping):
            return cls._sanitize_log_value(files)

        summaries: dict[Any, Any] = {}
        for field_name, file_spec in files.items():
            if isinstance(file_spec, tuple) and file_spec:
                summary: dict[str, Any] = {"文件名": str(file_spec[0])}
                if len(file_spec) >= 2:
                    content = file_spec[1]
                    if isinstance(content, (bytes, bytearray, memoryview)):
                        summary["大小"] = len(content)
                    elif hasattr(content, "read"):
                        summary["内容"] = cls._sanitize_log_value(content)
                if len(file_spec) >= 3:
                    summary["内容类型"] = str(file_spec[2])
                summaries[field_name] = summary
            else:
                summaries[field_name] = cls._sanitize_log_value(file_spec)
        return summaries

    @classmethod
    def _request_log_context(cls, kwargs: Mapping[str, Any]) -> dict[str, Any]:
        context: dict[str, Any] = {}
        for name in ("params", "json", "data"):
            if name in kwargs:
                context[name] = cls._sanitize_log_value(kwargs[name])
        if "files" in kwargs:
            context["files"] = cls._summarize_log_files(kwargs["files"])
        return context

    def _request(self, method: str, path: str, **kwargs: Any) -> Response:
        custom_headers = dict(kwargs.pop("headers", {}) or {})
        headers = {**custom_headers, **self.auth.build_headers()}
        url = f"{self.api_url}{path}"
        normalized_method = method.upper()
        started_at = time.monotonic()
        if LOGGER.isEnabledFor(logging.DEBUG):
            LOGGER.debug(
                "请求开始 method=%s url=%s context=%s",
                normalized_method,
                url,
                self._request_log_context(kwargs),
            )
        try:
            response = self.session.request(
                normalized_method,
                url,
                headers=headers,
                timeout=kwargs.pop("timeout", self.timeout),
                verify=kwargs.pop("verify", self.verify),
                **kwargs,
            )
        except requests.RequestException as exc:
            LOGGER.debug(
                "请求失败 method=%s url=%s elapsed=%.3fs error_type=%s",
                normalized_method,
                url,
                time.monotonic() - started_at,
                type(exc).__name__,
            )
            raise MyObjHTTPError(f"请求 MyObj 失败: {exc}") from exc
        LOGGER.debug(
            "请求完成 method=%s url=%s status_code=%s elapsed=%.3fs",
            normalized_method,
            url,
            response.status_code,
            time.monotonic() - started_at,
        )
        return response

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
        LOGGER.debug(
            "业务响应 method=%s path=%s status_code=%s code=%s message=%s",
            method.upper(),
            path,
            response.status_code,
            code,
            message,
        )

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

    @staticmethod
    def _tag_filter(tag_ids: Sequence[str], tag_mode: str) -> tuple[str, str]:
        normalized = list(
            dict.fromkeys(tag_id.strip() for tag_id in tag_ids if tag_id.strip())
        )
        mode = tag_mode.lower().strip() or "all"
        if mode not in ("all", "any"):
            raise ValueError("tag_mode 仅支持 all 或 any")
        return ",".join(normalized), mode

    @staticmethod
    def _check_polling(initial_interval: float, max_interval: float) -> None:
        if initial_interval <= 0:
            raise ValueError("poll_interval 必须大于0")
        if max_interval < initial_interval:
            raise ValueError("max_poll_interval 不能小于 poll_interval")

    @staticmethod
    def _sleep_with_backoff(
        interval: float,
        max_interval: float,
        deadline: Optional[float] = None,
    ) -> float:
        delay = interval
        if deadline is not None:
            delay = min(delay, max(0.0, deadline - time.monotonic()))
        time.sleep(delay)
        return min(max_interval, interval * 1.5)

    # 文件与目录

    def list_files(
        self,
        *,
        directory_id: int = 0,
        file_type: str = "",
        sort_by: str = "",
        sort_order: str = "",
        tag_ids: Sequence[str] = (),
        tag_mode: str = "all",
        page: int = 1,
        page_size: int = 20,
    ) -> dict[str, Any]:
        """获取当前用户指定目录下的文件列表。"""

        self._check_page(page, page_size)
        tags, mode = self._tag_filter(tag_ids, tag_mode)
        return self._request_json(
            "GET",
            "/file/list",
            params=self._params(
                directory_id=directory_id,
                type=file_type,
                sortBy=sort_by,
                sortOrder=sort_order,
                tag_ids=tags,
                tag_mode=mode if tags else "",
                page=page,
                pageSize=page_size,
            ),
        )

    def search_files(
        self,
        keyword: str = "",
        *,
        directory_id: int = 0,
        file_type: str = "",
        sort_by: str = "",
        sort_order: str = "",
        tag_ids: Sequence[str] = (),
        tag_mode: str = "all",
        page: int = 1,
        page_size: int = 20,
    ) -> dict[str, Any]:
        """搜索当前用户的文件。"""

        self._check_page(page, page_size)
        tags, mode = self._tag_filter(tag_ids, tag_mode)
        if not keyword.strip() and not tags:
            raise ValueError("keyword 或 tag_ids 至少提供一项")
        return self._request_json(
            "GET",
            "/file/search/user",
            params=self._params(
                keyword=keyword,
                directory_id=directory_id if directory_id > 0 else "",
                type=file_type,
                sortBy=sort_by,
                sortOrder=sort_order,
                tag_ids=tags,
                tag_mode=mode if tags else "",
                page=page,
                pageSize=page_size,
            ),
        )

    def search_public_files(
        self,
        keyword: str = "",
        *,
        file_type: str = "",
        sort_by: str = "",
        sort_order: str = "",
        tag_ids: Sequence[str] = (),
        tag_mode: str = "all",
        page: int = 1,
        page_size: int = 20,
    ) -> dict[str, Any]:
        """搜索公开文件。当前服务端仍要求认证。"""

        self._check_page(page, page_size)
        tags, mode = self._tag_filter(tag_ids, tag_mode)
        if not keyword.strip() and not tags:
            raise ValueError("keyword 或 tag_ids 至少提供一项")
        return self._request_json(
            "GET",
            "/file/search/public",
            params=self._params(
                keyword=keyword,
                type=file_type,
                sortBy=sort_by,
                sortOrder=sort_order,
                tag_ids=tags,
                tag_mode=mode if tags else "",
                page=page,
                pageSize=page_size,
            ),
        )

    def list_public_files(
        self,
        *,
        file_type: str = "",
        sort_by: str = "",
        tag_ids: Sequence[str] = (),
        tag_mode: str = "all",
        page: int = 1,
        page_size: int = 20,
    ) -> dict[str, Any]:
        """获取文件广场列表。"""

        self._check_page(page, page_size)
        tags, mode = self._tag_filter(tag_ids, tag_mode)
        return self._request_json(
            "GET",
            "/file/public/list",
            params=self._params(
                type=file_type,
                sortBy=sort_by,
                tag_ids=tags,
                tag_mode=mode if tags else "",
                page=page,
                pageSize=page_size,
            ),
        )

    def get_file_tags(self, file_id: str) -> dict[str, Any]:
        """读取用户文件的有效、已屏蔽及来源标签。"""

        return self._request_json("GET", f"/file/tags/{file_id}")

    def get_tag_suggestions(
        self,
        keyword: str = "",
        *,
        limit: int = 20,
    ) -> dict[str, Any]:
        """返回当前用户使用过及允许公开的标签建议。"""

        if not 1 <= limit <= 50:
            raise ValueError("limit 必须在 1 到 50 之间")
        return self._request_json(
            "GET",
            "/file/tags/suggestions",
            params=self._params(keyword=keyword, limit=limit),
        )

    def retry_file_tags(self, file_id: str) -> dict[str, Any]:
        """将单个文件的自动标签生成任务重新排队。"""

        return self._request_json("POST", f"/file/tags/{file_id}/retry")

    def update_manual_tags(
        self,
        file_id: str,
        *,
        add: Sequence[Mapping[str, Any]] = (),
        remove_tag_ids: Sequence[str] = (),
    ) -> dict[str, Any]:
        """增加、删除手工标签并设置分类和公开性。"""

        return self._request_json(
            "PUT",
            f"/file/tags/{file_id}/manual",
            json={
                "add": [dict(item) for item in add],
                "remove_tag_ids": list(remove_tag_ids),
            },
        )

    def update_tag_exclusions(
        self,
        file_id: str,
        *,
        suppress_tag_ids: Sequence[str] = (),
        restore_tag_ids: Sequence[str] = (),
    ) -> dict[str, Any]:
        """屏蔽或恢复自动标签。"""

        return self._request_json(
            "PUT",
            f"/file/tags/{file_id}/exclusions",
            json={
                "suppress_tag_ids": list(suppress_tag_ids),
                "restore_tag_ids": list(restore_tag_ids),
            },
        )

    def batch_update_tags(
        self,
        file_ids: Sequence[str],
        *,
        add: Sequence[Mapping[str, Any]] = (),
        remove_tag_ids: Sequence[str] = (),
    ) -> dict[str, Any]:
        """对最多 100 个用户文件原子增删手工标签。"""

        ids = list(dict.fromkeys(file_ids))
        if not 1 <= len(ids) <= 100:
            raise ValueError("file_ids 数量必须在 1 到 100 之间")
        return self._request_json(
            "POST",
            "/file/tags/batch",
            json={
                "file_ids": ids,
                "add": [dict(item) for item in add],
                "remove_tag_ids": list(remove_tag_ids),
            },
        )

    def get_directories(self) -> dict[str, Any]:
        """获取当前用户的虚拟目录树。"""

        return self._request_json("GET", "/file/directories")

    def create_directory(self, parent_id: int, name: str) -> dict[str, Any]:
        """创建目录。"""

        return self._request_json(
            "POST",
            "/file/makeDir",
            json={"parent_id": parent_id, "name": name},
        )

    def ensure_directory(
        self,
        parent_id: int,
        name: str,
        *,
        page_size: int = 100,
    ) -> int:
        """查找目录，不存在时创建，并返回目录 ID。"""

        if not name.strip():
            raise ValueError("name 不能为空")
        self._check_page(1, page_size)

        def find_directory() -> Optional[int]:
            page = 1
            while True:
                result = self.list_files(
                    directory_id=parent_id,
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
                        if folder.get("name") == name and folder.get("id") is not None:
                            return int(folder["id"])

                total = int(data.get("total", 0) or 0)
                actual_page_size = int(data.get("page_size", page_size) or page_size)
                if page * actual_page_size >= total:
                    return None
                page += 1

        existing_path = find_directory()
        if existing_path is not None:
            return existing_path

        self.create_directory(parent_id, name)
        created_path = find_directory()
        if created_path is None:
            raise MyObjHTTPError(f"目录 {name} 创建成功，但未能查询到目录 ID")
        return created_path

    def move_file(self, file_id: str, target_directory_id: int) -> dict[str, Any]:
        """移动文件。"""

        return self._request_json(
            "POST",
            "/file/move",
            json={
                "file_id": file_id,
                "target_directory_id": target_directory_id,
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

    def update_thumbnail(
        self, file_id: str, thumbnail_path: PathLike
    ) -> dict[str, Any]:
        """使用 JPEG 图片修改文件缩略图。"""

        thumbnail = Path(thumbnail_path)
        if not thumbnail.is_file():
            raise FileNotFoundError(f"缩略图不存在: {thumbnail}")
        with thumbnail.open("rb") as thumbnail_stream:
            return self._request_json(
                "PUT",
                f"/file/thumbnail/{file_id}",
                files={
                    "thumbnail": (
                        thumbnail.name,
                        thumbnail_stream,
                        "image/jpeg",
                    )
                },
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
        directory_id: int,
        *,
        encrypted: bool = False,
        file_password: str = "",
        thumbnail_path: Optional[PathLike] = None,
        progress: Optional[ProgressCallback] = None,
        show_progress: bool = True,
        wait_for_completion: bool = False,
        finalize_timeout: Optional[float] = None,
        finalize_poll_interval: float = 1.0,
        finalize_max_poll_interval: float = 5.0,
    ) -> dict[str, Any]:
        """预检并上传文件，默认在分片上传完成后立即返回后台处理状态。"""

        source_path = Path(file_path)
        if not source_path.is_file():
            raise FileNotFoundError(f"文件不存在: {source_path}")
        if encrypted and not file_password:
            raise ValueError("加密上传必须提供 file_password")
        if finalize_timeout is not None and finalize_timeout <= 0:
            raise ValueError("finalize_timeout 必须大于0")
        self._check_polling(
            finalize_poll_interval,
            finalize_max_poll_interval,
        )

        chunk_size = self.DEFAULT_CHUNK_SIZE
        file_size = source_path.stat().st_size
        file_md5, chunk_md5s = self._hash_file(source_path, chunk_size)
        with ExitStack() as upload_stack:
            progress_bar = None
            if show_progress:
                upload_stack.enter_context(
                    logging_redirect_tqdm(
                        loggers=[logging.getLogger(), LOGGER],
                        tqdm_class=tqdm,
                    )
                )
                progress_bar = upload_stack.enter_context(
                    tqdm(
                        total=file_size,
                        desc=f"上传 {source_path.name}",
                        unit="B",
                        unit_scale=True,
                        unit_divisor=1024,
                    )
                )

            reported_bytes = 0

            def report_progress(completed: int) -> None:
                nonlocal reported_bytes
                if progress_bar is not None:
                    progress_bar.update(max(0, completed - reported_bytes))
                    if completed == file_size:
                        progress_bar.refresh()
                reported_bytes = completed
                if progress:
                    progress(completed, file_size)

            precheck = self._request_json(
                "POST",
                "/file/upload/precheck",
                accepted_codes=(200, 201),
                json={
                    "chunk_signature": file_md5,
                    "file_name": source_path.name,
                    "file_size": file_size,
                    "files_md5": chunk_md5s,
                    "directory_id": directory_id,
                },
            )

            if int(precheck["code"]) == 200:
                report_progress(file_size)
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
                        report_progress(uploaded_bytes)
                        continue

                    form = {
                        "precheck_id": precheck_id,
                        "chunk_index": str(chunk_index),
                        "total_chunks": str(total_chunks),
                        "chunk_md5": chunk_md5,
                        "is_enc": "true" if encrypted else "false",
                        "file_password": file_password if encrypted else "",
                        "async_finalize": "true",
                    }
                    files: dict[str, Any] = {
                        "file": (
                            source_path.name,
                            chunk,
                            "application/octet-stream",
                        )
                    }

                    with ExitStack() as thumbnail_stack:
                        if thumbnail_path is not None and not thumbnail_sent:
                            thumbnail = Path(thumbnail_path)
                            thumbnail_stream = thumbnail_stack.enter_context(
                                thumbnail.open("rb")
                            )
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
                            timeout=self.timeout,
                        )
                        pending_chunks -= 1

                    uploaded_bytes += len(chunk)
                    report_progress(uploaded_bytes)

            response_data = last_response.get("data")
            if (
                wait_for_completion
                and isinstance(response_data, Mapping)
                and response_data.get("status") == "processing"
            ):
                return self._wait_upload_finalize(
                    precheck_id,
                    timeout=finalize_timeout,
                    poll_interval=finalize_poll_interval,
                    max_poll_interval=finalize_max_poll_interval,
                )
            return last_response

    def _wait_upload_finalize(
        self,
        precheck_id: str,
        *,
        timeout: Optional[float],
        poll_interval: float,
        max_poll_interval: float,
    ) -> dict[str, Any]:
        deadline = time.monotonic() + timeout if timeout is not None else None
        current_interval = poll_interval
        while True:
            progress_response = self.get_upload_progress(precheck_id)
            progress_data = progress_response.get("data")
            if not isinstance(progress_data, Mapping):
                raise MyObjHTTPError("服务器未返回文件处理进度")
            status = str(progress_data.get("status") or "")
            if status == "completed":
                file_id = str(progress_data.get("file_id") or "")
                return {
                    "code": 200,
                    "message": "上传成功",
                    "data": {
                        "id": file_id,
                        "file_id": file_id,
                        "is_complete": True,
                    },
                }
            if status in {"failed", "aborted"}:
                message = str(
                    progress_data.get("error_message") or "服务器处理文件失败"
                )
                raise MyObjHTTPError(message)
            if deadline is not None and time.monotonic() >= deadline:
                raise TimeoutError("等待服务器处理文件超时")
            current_interval = self._sleep_with_backoff(
                current_interval,
                max_poll_interval,
                deadline,
            )

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

    def get_local_download_task(self, task_id: str) -> dict[str, Any]:
        """查询网盘文件下载准备任务。"""

        return self._request_json("GET", f"/download/local/task/{task_id}")

    def download_file(
        self,
        file_id: str,
        destination: PathLike,
        *,
        file_password: str = "",
        prepare_timeout: float = 300.0,
        poll_interval: float = 0.5,
        max_poll_interval: float = 5.0,
        progress: Optional[ProgressCallback] = None,
    ) -> Path:
        """等待服务端完成文件准备后，将文件流式保存到指定路径。"""

        self._check_polling(poll_interval, max_poll_interval)
        task = self.create_file_download(file_id, file_password=file_password)
        data = task.get("data")
        if not isinstance(data, Mapping) or not data.get("task_id"):
            raise MyObjHTTPError("创建下载任务成功，但响应中没有 task_id")
        task_id = str(data["task_id"])

        deadline = time.monotonic() + prepare_timeout
        current_interval = poll_interval
        while True:
            state_response = self.get_local_download_task(task_id)
            state_data = state_response.get("data")
            state = state_data.get("state") if isinstance(state_data, Mapping) else None
            if state == 3:
                break
            if state == 4:
                message = str(state_data.get("error_msg") or "准备下载文件失败")
                raise MyObjAPIError(500, message, data=state_data)
            if state == 5:
                raise MyObjAPIError(400, "下载任务已取消", data=state_data)
            if time.monotonic() >= deadline:
                raise TimeoutError("等待 MyObj 准备下载文件超时")
            current_interval = self._sleep_with_backoff(
                current_interval,
                max_poll_interval,
                deadline,
            )

        response = self._request_binary("GET", f"/download/local/file/{task_id}")

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
        max_poll_interval: float = 5.0,
        progress: Optional[ProgressCallback] = None,
    ) -> Path:
        self._check_polling(poll_interval, max_poll_interval)
        deadline = time.monotonic() + wait_timeout
        current_interval = poll_interval
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
            current_interval = self._sleep_with_backoff(
                current_interval,
                max_poll_interval,
                deadline,
            )

        response = self._request_binary(
            "GET",
            "/file/package/download",
            params={"package_id": package_id},
        )
        return self._save_binary_response(response, destination, progress=progress)
