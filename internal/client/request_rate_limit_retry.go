package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type requestRateLimitVerdict interface {
	isRequestRateLimitVerdict()
}

type notRefusedByARequestRateLimit struct{}

type sendAgainAfter struct {
	wait time.Duration
}

type stopAtTheRequestRateLimit struct {
	err error
}

func (notRefusedByARequestRateLimit) isRequestRateLimitVerdict() {}
func (sendAgainAfter) isRequestRateLimitVerdict()                {}
func (stopAtTheRequestRateLimit) isRequestRateLimitVerdict()     {}

const mostWaitsForARequestRateLimit = 3

const longestRetryAfterSeconds = 60

type requestRateLimitRefusal struct {
	Sentence string `json:"error"`
	Code     string `json:"code"`
}

func requestRateLimitRefusalIn(answer apiAnswer) (requestRateLimitRefusal, bool) {
	if answer.statusCode != http.StatusTooManyRequests {
		return requestRateLimitRefusal{}, false
	}
	var refusal requestRateLimitRefusal
	if err := json.Unmarshal(answer.body, &refusal); err != nil || refusal.Code != "REQUEST_RATE_LIMIT_REACHED" {
		return requestRateLimitRefusal{}, false
	}
	return refusal, true
}

func requestRateLimitVerdictOn(answer apiAnswer, waitsMade int) requestRateLimitVerdict {
	refusal, refused := requestRateLimitRefusalIn(answer)
	if !refused {
		return notRefusedByARequestRateLimit{}
	}
	if waitsMade == mostWaitsForARequestRateLimit {
		return stopAtTheRequestRateLimit{err: fmt.Errorf("ConfigDirector kept refusing the request after three waits. %s", refusal.Sentence)}
	}
	wait, usable := waitNamedByRetryAfter(answer.header.Get("Retry-After"))
	if !usable {
		return stopAtTheRequestRateLimit{err: errors.New(refusal.Sentence)}
	}
	return sendAgainAfter{wait: wait}
}

func waitNamedByRetryAfter(retryAfter string) (time.Duration, bool) {
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil || seconds < 1 || seconds > longestRetryAfterSeconds {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}
