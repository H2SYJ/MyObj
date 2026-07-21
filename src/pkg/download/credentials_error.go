package download

import (
	"errors"
	"fmt"
)

// CredentialsRequiredError 表示远端拒绝当前HTTP/HLS凭据，应暂停任务等待更新请求头。
type CredentialsRequiredError struct {
	StatusCode int
	URL        string
	Reason     string
}

func (e *CredentialsRequiredError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return fmt.Sprintf("远端资源返回%d，请更新Cookie、Authorization或其他请求头后恢复任务", e.StatusCode)
}

func IsCredentialsRequired(err error) bool {
	var target *CredentialsRequiredError
	return errors.As(err, &target)
}

// 兼容旧名称，避免已有调用方和测试中断。
type HLSCredentialsRequiredError = CredentialsRequiredError

func IsHLSCredentialsRequired(err error) bool {
	return IsCredentialsRequired(err)
}
