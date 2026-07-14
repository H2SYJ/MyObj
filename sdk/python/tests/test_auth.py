import base64
import unittest
from urllib.parse import parse_qs

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa

from myobj_sdk.auth import ApiKeyAuth


class ApiKeyAuthTest(unittest.TestCase):
    def test_build_headers_can_be_decrypted_by_server_private_key(self) -> None:
        private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        public_key_pem = (
            private_key.public_key()
            .public_bytes(
                serialization.Encoding.PEM,
                serialization.PublicFormat.SubjectPublicKeyInfo,
            )
            .decode("utf-8")
        )
        auth = ApiKeyAuth("test-api-key", public_key_pem)

        headers = auth.build_headers(timestamp_ms=1234567890123, nonce="固定随机数")
        decrypted = private_key.decrypt(
            base64.b64decode(headers["X-Signature"]),
            padding.PKCS1v15(),
        ).decode("utf-8")
        values = parse_qs(decrypted)

        self.assertEqual(headers["X-API-Key"], "test-api-key")
        self.assertEqual(headers["X-Timestamp"], "1234567890123")
        self.assertEqual(headers["X-Nonce"], "固定随机数")
        self.assertEqual(values["apikey"], ["test-api-key"])
        self.assertEqual(values["timestamp"], ["1234567890123"])
        self.assertEqual(values["nonce"], ["固定随机数"])


if __name__ == "__main__":
    unittest.main()
