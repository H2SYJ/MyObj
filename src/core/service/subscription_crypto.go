package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"myobj/src/config"
	"strings"
)

func encryptSubscriptionConfig(subscriptionID, userID string, value map[string]interface{}) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	plaintext, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	aead, err := subscriptionAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nil, nonce, plaintext, []byte(subscriptionID+"\x00"+userID))
	return "v1:" + base64.RawStdEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func decryptSubscriptionConfig(subscriptionID, userID, encrypted string) (map[string]interface{}, error) {
	if encrypted == "" {
		return map[string]interface{}{}, nil
	}
	if !strings.HasPrefix(encrypted, "v1:") {
		return nil, fmt.Errorf("不支持的订阅配置密文版本")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encrypted, "v1:"))
	if err != nil {
		return nil, err
	}
	aead, err := subscriptionAEAD()
	if err != nil {
		return nil, err
	}
	if len(payload) < aead.NonceSize() {
		return nil, fmt.Errorf("订阅配置密文无效")
	}
	plaintext, err := aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], []byte(subscriptionID+"\x00"+userID))
	if err != nil {
		return nil, err
	}
	var value map[string]interface{}
	if err := json.Unmarshal(plaintext, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func subscriptionAEAD() (cipher.AEAD, error) {
	if config.CONFIG == nil || config.CONFIG.Auth.Secret == "" {
		return nil, fmt.Errorf("服务端认证密钥为空")
	}
	key := sha256.Sum256([]byte("myobj-subscription-config-v1\x00" + config.CONFIG.Auth.Secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
