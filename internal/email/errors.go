package email

import (
  "errors"
  "fmt"
)

type AuthError struct{ Err error }

func (e *AuthError) Error() string { return fmt.Sprintf("邮件认证失败: %v", e.Err) }
func (e *AuthError) Unwrap() error { return e.Err }

type NetworkError struct{ Err error }

func (e *NetworkError) Error() string { return fmt.Sprintf("邮件网络连接失败: %v", e.Err) }
func (e *NetworkError) Unwrap() error { return e.Err }

type ProtocolError struct{ Err error }

func (e *ProtocolError) Error() string { return fmt.Sprintf("邮件协议错误: %v", e.Err) }
func (e *ProtocolError) Unwrap() error { return e.Err }

type TimeoutError struct{ Err error }

func (e *TimeoutError) Error() string { return fmt.Sprintf("邮件超时: %v", e.Err) }
func (e *TimeoutError) Unwrap() error { return e.Err }

func IsAuthError(err error) bool {
  var e *AuthError
  return errors.As(err, &e)
}

func IsNetworkError(err error) bool {
  var e *NetworkError
  return errors.As(err, &e)
}

func IsProtocolError(err error) bool {
  var e *ProtocolError
  return errors.As(err, &e)
}
