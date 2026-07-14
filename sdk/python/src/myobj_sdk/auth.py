"""MyObj API Key 请求认证。"""

from __future__ import annotations

import base64
import time
import uuid
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional, Union
from urllib.parse import urlencode

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa


PathLike = Union[str, Path]


@dataclass(frozen=True)
class ApiKeyAuth:
    """根据 MyObj 的 RSA 加密协议生成认证请求头。"""

    api_key: str
    public_key_pem: str
    _public_key: rsa.RSAPublicKey = field(init=False, repr=False, compare=False)

    def __post_init__(self) -> None:
        if not self.api_key.strip():
            raise ValueError("api_key 不能为空")

        key = serialization.load_pem_public_key(self.public_key_pem.encode("utf-8"))
        if not isinstance(key, rsa.RSAPublicKey):
            raise ValueError("public_key_pem 必须是 RSA 公钥")
        object.__setattr__(self, "_public_key", key)

    @classmethod
    def from_file(cls, api_key: str, public_key_path: PathLike) -> "ApiKeyAuth":
        """从 UTF-8 PEM 文件加载公钥。"""

        public_key_pem = Path(public_key_path).read_text(encoding="utf-8")
        return cls(api_key=api_key, public_key_pem=public_key_pem)

    def build_headers(
        self,
        *,
        timestamp_ms: Optional[int] = None,
        nonce: Optional[str] = None,
    ) -> dict[str, str]:
        """为单次请求生成四个认证请求头。"""

        timestamp = str(
            timestamp_ms if timestamp_ms is not None else int(time.time() * 1000)
        )
        request_nonce = nonce or uuid.uuid4().hex
        if not request_nonce.strip():
            raise ValueError("nonce 不能为空")

        plaintext = urlencode(
            {
                "apikey": self.api_key,
                "timestamp": timestamp,
                "nonce": request_nonce,
            }
        )
        encrypted = self._public_key.encrypt(
            plaintext.encode("utf-8"),
            padding.PKCS1v15(),
        )

        return {
            "X-API-Key": self.api_key,
            "X-Signature": base64.b64encode(encrypted).decode("ascii"),
            "X-Timestamp": timestamp,
            "X-Nonce": request_nonce,
        }
