package client

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"
)

type connectionFailure int

const (
	connectionNeverOpened connectionFailure = iota
	connectionDropped
	connectionFailureNotTransient
)

var waitsBeforeEachRetry = []time.Duration{500 * time.Millisecond, time.Second}

var repeatableMethods = map[string]bool{http.MethodGet: true, http.MethodPut: true, http.MethodDelete: true}

var retriesAfter = map[connectionFailure]func(method string) bool{
	connectionNeverOpened:         func(string) bool { return true },
	connectionDropped:             func(method string) bool { return repeatableMethods[method] },
	connectionFailureNotTransient: func(string) bool { return false },
}

func connectionFailureOf(err error) connectionFailure {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return connectionFailureNotTransient
	}
	var lookupError *net.DNSError
	if errors.As(err, &lookupError) {
		if lookupError.IsTemporary || lookupError.IsTimeout {
			return connectionNeverOpened
		}
		return connectionFailureNotTransient
	}
	var operationError *net.OpError
	if errors.As(err, &operationError) && operationError.Op == "dial" {
		return connectionNeverOpened
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return connectionDropped
	}
	return connectionFailureNotTransient
}

func retryWaitAfter(method string, err error, attemptsMade int) (time.Duration, bool) {
	if attemptsMade > len(waitsBeforeEachRetry) || !retriesAfter[connectionFailureOf(err)](method) {
		return 0, false
	}
	return waitsBeforeEachRetry[attemptsMade-1], true
}
