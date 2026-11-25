package client

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"
)

type waitRecorder struct {
	waits []time.Duration
}

func (recorder *waitRecorder) wait(_ context.Context, wait time.Duration) error {
	recorder.waits = append(recorder.waits, wait)
	return nil
}

func startStubAPIRecordingWaits(t *testing.T, replies ...stubReply) (*stubAPI, *Client, *waitRecorder) {
	t.Helper()

	stub, client := startStubAPI(t, replies...)
	recorder := &waitRecorder{}
	client.waitBeforeSendingAgain = recorder.wait
	return stub, client, recorder
}

func answerWithRetryAfter(status int, retryAfter string, body string) stubReply {
	return func(t *testing.T, response http.ResponseWriter) {
		response.Header().Set("Retry-After", retryAfter)
		answer(status, body)(t, response)
	}
}

func closingTheConnectionAfter(reply stubReply) stubReply {
	return func(t *testing.T, response http.ResponseWriter) {
		response.Header().Set("Connection", "close")
		reply(t, response)
	}
}

func requestRateLimitRefusalJSON(sentence string) string {
	return `{"error":"` + sentence + `","code":"REQUEST_RATE_LIMIT_REACHED"}`
}

func requireRequestBodies(t *testing.T, stub *stubAPI, expected ...string) {
	t.Helper()

	if received := stub.receivedRequestBodies(); !slices.Equal(received, expected) {
		t.Fatalf("expected bodies %q, received %q", expected, received)
	}
}

func requireWaits(t *testing.T, recorder *waitRecorder, expected ...time.Duration) {
	t.Helper()

	if !slices.Equal(recorder.waits, expected) {
		t.Fatalf("expected waits %v, the client asked for %v", expected, recorder.waits)
	}
}

func TestRequestRateLimit_getIsSentAgainAfterTheWaitTheAPINames(t *testing.T) {
	stub, client, recorder := startStubAPIRecordingWaits(t,
		answerWithRetryAfter(http.StatusTooManyRequests, "23", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 23 seconds.")),
		answer(http.StatusOK, storefrontJSON),
	)

	project, err := client.GetProject(context.Background(), storefrontID)

	if err != nil {
		t.Fatal(err)
	}
	if project.Slug != "storefront" {
		t.Fatalf("expected the storefront project, got %+v", project)
	}
	requireRequestLines(t, stub, "GET "+storefrontPath, "GET "+storefrontPath)
	requireWaits(t, recorder, 23*time.Second)
}

func TestRequestRateLimit_postIsSentAgainWithTheSameBody(t *testing.T) {
	stub, client, recorder := startStubAPIRecordingWaits(t,
		answerWithRetryAfter(http.StatusTooManyRequests, "7", requestRateLimitRefusalJSON("This client address has reached the limit of 3,000 requests per minute. Retry after 7 seconds.")),
		answer(http.StatusCreated, storefrontJSON),
	)

	project, err := client.CreateProject(context.Background(), CreateProjectRequest{Name: "Storefront", Slug: "storefront"})

	if err != nil {
		t.Fatal(err)
	}
	if project.ID != storefrontID {
		t.Fatalf("expected the created storefront project, got %+v", project)
	}
	requireRequestLines(t, stub, "POST /v1/projects", "POST /v1/projects")
	requireRequestBodies(t, stub, `{"name":"Storefront","slug":"storefront"}`, `{"name":"Storefront","slug":"storefront"}`)
	requireWaits(t, recorder, 7*time.Second)
}

func TestRequestRateLimit_failsWhenTheAPIKeepsRefusingAfterThreeWaits(t *testing.T) {
	stub, client, recorder := startStubAPIRecordingWaits(t,
		answerWithRetryAfter(http.StatusTooManyRequests, "41", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 41 seconds.")),
		answerWithRetryAfter(http.StatusTooManyRequests, "60", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 60 seconds.")),
		answerWithRetryAfter(http.StatusTooManyRequests, "12", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 12 seconds.")),
		answerWithRetryAfter(http.StatusTooManyRequests, "59", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 59 seconds.")),
		answer(http.StatusOK, storefrontJSON),
	)

	_, err := client.GetProject(context.Background(), storefrontID)

	if err == nil {
		t.Fatal("expected the refusals to fail the read")
	}
	expected := "ConfigDirector kept refusing the request after three waits. This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 59 seconds."
	if err.Error() != expected {
		t.Fatalf("expected the error %q, got %q", expected, err.Error())
	}
	requireRequestLines(t, stub, "GET "+storefrontPath, "GET "+storefrontPath, "GET "+storefrontPath, "GET "+storefrontPath)
	requireWaits(t, recorder, 41*time.Second, 60*time.Second, 12*time.Second)
}

func TestRequestRateLimit_aRefusalWithoutRetryAfterFailsWithTheAPISentence(t *testing.T) {
	stub, client, recorder := startStubAPIRecordingWaits(t,
		answer(http.StatusTooManyRequests, requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 23 seconds.")),
		answer(http.StatusOK, storefrontJSON),
	)

	_, err := client.GetProject(context.Background(), storefrontID)

	if err == nil {
		t.Fatal("expected the refusal to fail the read")
	}
	expected := "This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 23 seconds."
	if err.Error() != expected {
		t.Fatalf("expected the error %q, got %q", expected, err.Error())
	}
	requireRequestLines(t, stub, "GET "+storefrontPath)
	requireWaits(t, recorder)
}

func TestRequestRateLimit_aRefusalWhoseRetryAfterIsNotAWholeNumberFailsWithTheAPISentence(t *testing.T) {
	stub, client, recorder := startStubAPIRecordingWaits(t,
		answerWithRetryAfter(http.StatusTooManyRequests, "soon", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 23 seconds.")),
		answer(http.StatusOK, storefrontJSON),
	)

	_, err := client.GetProject(context.Background(), storefrontID)

	if err == nil {
		t.Fatal("expected the refusal to fail the read")
	}
	expected := "This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 23 seconds."
	if err.Error() != expected {
		t.Fatalf("expected the error %q, got %q", expected, err.Error())
	}
	requireRequestLines(t, stub, "GET "+storefrontPath)
	requireWaits(t, recorder)
}

func TestRequestRateLimit_aRefusalWhoseRetryAfterIsLongerThanAMinuteFailsWithTheAPISentence(t *testing.T) {
	stub, client, recorder := startStubAPIRecordingWaits(t,
		answerWithRetryAfter(http.StatusTooManyRequests, "600", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 600 seconds.")),
		answer(http.StatusOK, storefrontJSON),
	)

	_, err := client.GetProject(context.Background(), storefrontID)

	if err == nil {
		t.Fatal("expected the refusal to fail the read")
	}
	expected := "This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 600 seconds."
	if err.Error() != expected {
		t.Fatalf("expected the error %q, got %q", expected, err.Error())
	}
	requireRequestLines(t, stub, "GET "+storefrontPath)
	requireWaits(t, recorder)
}

func TestRequestRateLimit_aRefusalWhoseRetryAfterIsZeroFailsWithTheAPISentence(t *testing.T) {
	stub, client, recorder := startStubAPIRecordingWaits(t,
		answerWithRetryAfter(http.StatusTooManyRequests, "0", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 0 seconds.")),
		answer(http.StatusOK, storefrontJSON),
	)

	_, err := client.GetProject(context.Background(), storefrontID)

	if err == nil {
		t.Fatal("expected the refusal to fail the read")
	}
	expected := "This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 0 seconds."
	if err.Error() != expected {
		t.Fatalf("expected the error %q, got %q", expected, err.Error())
	}
	requireRequestLines(t, stub, "GET "+storefrontPath)
	requireWaits(t, recorder)
}

func TestRequestRateLimit_cancelingTheRunEndsTheWait(t *testing.T) {
	stub, client := startStubAPI(t,
		answerWithRetryAfter(http.StatusTooManyRequests, "60", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 60 seconds.")),
		answer(http.StatusOK, storefrontJSON),
	)
	waitStarted := make(chan struct{})
	waitUnlessCanceled := client.waitBeforeSendingAgain
	client.waitBeforeSendingAgain = func(ctx context.Context, wait time.Duration) error {
		close(waitStarted)
		return waitUnlessCanceled(ctx, wait)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	readEnded := make(chan error, 1)

	go func() {
		_, err := client.GetProject(ctx, storefrontID)
		readEnded <- err
	}()
	select {
	case <-waitStarted:
	case err := <-readEnded:
		t.Fatalf("expected the client to wait before sending the request again, the read ended with %v", err)
	}
	cancel()

	select {
	case err := <-readEnded:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected the context's cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected canceling the run to end the 60-second wait")
	}
	requireRequestLines(t, stub, "GET "+storefrontPath)
}

func TestRequestRateLimit_aSuspendedServiceIsNotWaitedOn(t *testing.T) {
	stub, client, recorder := startStubAPIRecordingWaits(t,
		answerWithRetryAfter(http.StatusTooManyRequests, "300", `{"error":"Service suspended","code":"SERVICE_SUSPENDED"}`),
		answer(http.StatusOK, storefrontJSON),
	)

	_, err := client.GetProject(context.Background(), storefrontID)

	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("expected an APIError, got %v", err)
	}
	if apiError.StatusCode != http.StatusTooManyRequests || apiError.Body != `{"error":"Service suspended","code":"SERVICE_SUSPENDED"}` {
		t.Fatalf("expected the suspension's 429 and body, got %d %q", apiError.StatusCode, apiError.Body)
	}
	requireRequestLines(t, stub, "GET "+storefrontPath)
	requireWaits(t, recorder)
}

func TestRequestRateLimit_eachAttemptKeepsItsOwnConnectionFailureRetries(t *testing.T) {
	stub, client, recorder := startStubAPIRecordingWaits(t,
		resetConnection,
		resetConnection,
		closingTheConnectionAfter(answerWithRetryAfter(http.StatusTooManyRequests, "23", requestRateLimitRefusalJSON("This organization has reached its limit of 1,200 Admin API requests per minute. Retry after 23 seconds."))),
		resetConnection,
		resetConnection,
		answer(http.StatusOK, storefrontJSON),
	)

	project, err := client.GetProject(context.Background(), storefrontID)

	if err != nil {
		t.Fatal(err)
	}
	if project.Slug != "storefront" {
		t.Fatalf("expected the storefront project, got %+v", project)
	}
	requireRequestLines(t, stub,
		"GET "+storefrontPath, "GET "+storefrontPath, "GET "+storefrontPath,
		"GET "+storefrontPath, "GET "+storefrontPath, "GET "+storefrontPath,
	)
	requireWaits(t, recorder, 500*time.Millisecond, time.Second, 23*time.Second, 500*time.Millisecond, time.Second)
}
