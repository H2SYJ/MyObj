"""MyObj SDK 异常类型。"""

from __future__ import annotations

from typing import Any, Optional


class MyObjError(Exception):
    """MyObj SDK 基础异常。"""


class MyObjHTTPError(MyObjError):
    """HTTP 或网络层异常。"""

    def __init__(self, message: str, *, status_code: Optional[int] = None) -> None:
        super().__init__(message)
        self.status_code = status_code


class MyObjAPIError(MyObjError):
    """MyObj 返回的业务错误。"""

    def __init__(
        self,
        code: int,
        message: str,
        *,
        data: Any = None,
        status_code: Optional[int] = None,
    ) -> None:
        super().__init__(f"MyObj API 错误 {code}: {message}")
        self.code = code
        self.message = message
        self.data = data
        self.status_code = status_code
