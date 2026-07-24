package platform

import (
	"errors"
	"strings"
)

type FailureCode string

const (
	FailureUnknown           FailureCode = "UNKNOWN"
	FailureCancelled         FailureCode = "CANCELLED"
	FailureInvalidForward    FailureCode = "INVALID_FORWARD"
	FailureKnownHosts        FailureCode = "KNOWN_HOSTS_ERROR"
	FailureHostKeyUnknown    FailureCode = "HOST_KEY_UNKNOWN"
	FailureHostKeyNotCached  FailureCode = "HOST_KEY_NOT_CACHED"
	FailureSSHAuth           FailureCode = "SSH_AUTH_FAILED"
	FailureSSHConnection     FailureCode = "SSH_CONNECTION_FAILED"
	FailureSSHConnectionLost FailureCode = "SSH_CONNECTION_LOST"
	FailureSSHKeepalive      FailureCode = "SSH_KEEPALIVE_FAILED"
	FailureRemoteListen      FailureCode = "REMOTE_LISTEN_FAILED"
	FailureUDPRelay          FailureCode = "UDP_RELAY_FAILED"
)

type Failure struct {
	Code   FailureCode
	Op     string
	Detail string
	Cause  error
}

func (f *Failure) Error() string {
	if f == nil {
		return ""
	}
	detail := strings.TrimSpace(f.Detail)
	if detail == "" && f.Cause != nil {
		detail = f.Cause.Error()
	}
	if detail == "" {
		return string(f.Code)
	}
	if f.Code == FailureUnknown {
		return detail
	}
	return string(f.Code) + ": " + detail
}

func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Cause
}

func NewFailure(code FailureCode, op string, cause error) *Failure {
	failure := &Failure{Code: code, Op: op, Cause: cause}
	if cause != nil {
		failure.Detail = cause.Error()
	}
	return failure
}

func FailureFromError(err error) *Failure {
	if err == nil {
		return nil
	}
	var failure *Failure
	if errors.As(err, &failure) {
		copy := *failure
		return &copy
	}
	return ParseFailure(err.Error())
}

func ParseFailure(message string) *Failure {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	codeText, detail, found := strings.Cut(message, ":")
	code := FailureCode(strings.TrimSpace(codeText))
	if !found || !knownFailureCode(code) {
		if strings.EqualFold(message, "cancelled") || strings.EqualFold(message, "canceled") {
			return &Failure{Code: FailureCancelled, Detail: message}
		}
		return &Failure{Code: FailureUnknown, Detail: message}
	}
	return &Failure{Code: code, Detail: strings.TrimSpace(detail)}
}

func knownFailureCode(code FailureCode) bool {
	switch code {
	case FailureInvalidForward,
		FailureKnownHosts,
		FailureHostKeyUnknown,
		FailureHostKeyNotCached,
		FailureSSHAuth,
		FailureSSHConnection,
		FailureSSHConnectionLost,
		FailureSSHKeepalive,
		FailureRemoteListen,
		FailureUDPRelay:
		return true
	default:
		return false
	}
}
