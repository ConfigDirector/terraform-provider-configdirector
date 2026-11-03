package client

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const storefrontID = "5a1c9e2f-3b4d-4e6f-8a7b-9c0d1e2f3a4b"
const storefrontPath = "/v1/projects/" + storefrontID
const storefrontJSON = `{"id":"` + storefrontID + `","name":"Storefront","slug":"storefront","organizationId":"7c3e1a4b-5d6f-4081-8c9d-1e2f3a4b5c6d","deletionProtection":false,"createdAt":"2026-11-01T10:00:00.000Z","updatedAt":"2026-11-01T10:00:00.000Z"}`

type stubReply func(t *testing.T, response http.ResponseWriter)

func resetConnection(t *testing.T, response http.ResponseWriter) {
	connection, _, err := response.(http.Hijacker).Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	if err := connection.(*net.TCPConn).SetLinger(0); err != nil {
		t.Error(err)
	}
	_ = connection.Close()
}

func closeConnection(t *testing.T, response http.ResponseWriter) {
	connection, _, err := response.(http.Hijacker).Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	_ = connection.Close()
}

func answer(status int, body string) stubReply {
	return func(t *testing.T, response http.ResponseWriter) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(status)
		_, _ = response.Write([]byte(body))
	}
}

type stubAPI struct {
	server       *httptest.Server
	mutex        sync.Mutex
	requestLines []string
}

func newStubAPI(t *testing.T, replies ...stubReply) *stubAPI {
	t.Helper()

	stub := &stubAPI{}
	stub.server = httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = io.ReadAll(request.Body)
		stub.mutex.Lock()
		stub.requestLines = append(stub.requestLines, request.Method+" "+request.URL.Path)
		attempt := len(stub.requestLines)
		stub.mutex.Unlock()
		replies[min(attempt, len(replies))-1](t, response)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (stub *stubAPI) receivedRequestLines() []string {
	stub.mutex.Lock()
	defer stub.mutex.Unlock()

	return slices.Clone(stub.requestLines)
}

func startStubAPI(t *testing.T, replies ...stubReply) (*stubAPI, *Client) {
	t.Helper()

	stub := newStubAPI(t, replies...)
	stub.server.Start()
	return stub, New(stub.server.URL, "connection-failure-test-token")
}

func startStubAPIAfterRefusingConnections(t *testing.T, refusal time.Duration, replies ...stubReply) (*stubAPI, *Client) {
	t.Helper()

	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	stub := newStubAPI(t, replies...)
	started := make(chan struct{})
	go func() {
		defer close(started)
		time.Sleep(refusal)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Error(err)
			return
		}
		stub.server.Listener = listener
		stub.server.Start()
	}()
	t.Cleanup(func() { <-started })
	return stub, New("http://"+address, "connection-failure-test-token")
}

func requireRequestLines(t *testing.T, stub *stubAPI, expected ...string) {
	t.Helper()

	if received := stub.receivedRequestLines(); !slices.Equal(received, expected) {
		t.Fatalf("expected requests %q, received %q", expected, received)
	}
}

func TestConnectionFailures_getRepeatsAfterTheNetworkResetsTheConnection(t *testing.T) {
	stub, client := startStubAPI(t, resetConnection, answer(http.StatusOK, storefrontJSON))

	project, err := client.GetProject(context.Background(), storefrontID)

	if err != nil {
		t.Fatal(err)
	}
	if project.Slug != "storefront" {
		t.Fatalf("expected the storefront project, got %+v", project)
	}
	requireRequestLines(t, stub, "GET "+storefrontPath, "GET "+storefrontPath)
}

func TestConnectionFailures_getRepeatsWhenTheConnectionClosesBeforeTheAnswer(t *testing.T) {
	stub, client := startStubAPI(t, closeConnection, answer(http.StatusOK, storefrontJSON))

	project, err := client.GetProject(context.Background(), storefrontID)

	if err != nil {
		t.Fatal(err)
	}
	if project.Slug != "storefront" {
		t.Fatalf("expected the storefront project, got %+v", project)
	}
	requireRequestLines(t, stub, "GET "+storefrontPath, "GET "+storefrontPath)
}

func TestConnectionFailures_putRepeatsAfterTheNetworkResetsTheConnection(t *testing.T) {
	stub, client := startStubAPI(t, resetConnection, answer(http.StatusNoContent, ""), answer(http.StatusOK, storefrontJSON))

	_, err := client.UpdateProject(context.Background(), storefrontID, UpdateProjectRequest{ID: storefrontID, Name: "Storefront", Slug: "storefront"})

	if err != nil {
		t.Fatal(err)
	}
	requireRequestLines(t, stub, "PUT "+storefrontPath, "PUT "+storefrontPath, "GET "+storefrontPath)
}

func TestConnectionFailures_deleteRepeatsAfterTheNetworkResetsTheConnection(t *testing.T) {
	stub, client := startStubAPI(t, resetConnection, answer(http.StatusNoContent, ""))

	err := client.DeleteProject(context.Background(), storefrontID)

	if err != nil {
		t.Fatal(err)
	}
	requireRequestLines(t, stub, "DELETE "+storefrontPath, "DELETE "+storefrontPath)
}

func TestConnectionFailures_postIsNotSentAgainAfterTheNetworkResetsTheConnection(t *testing.T) {
	stub, client := startStubAPI(t, resetConnection, answer(http.StatusCreated, storefrontJSON))

	_, err := client.CreateProject(context.Background(), CreateProjectRequest{Name: "Storefront", Slug: "storefront"})

	if err == nil {
		t.Fatal("expected the reset to fail the creation")
	}
	requireRequestLines(t, stub, "POST /v1/projects")
}

func TestConnectionFailures_postRepeatsWhenTheConnectionWasRefused(t *testing.T) {
	stub, client := startStubAPIAfterRefusingConnections(t, 100*time.Millisecond, answer(http.StatusCreated, storefrontJSON))

	project, err := client.CreateProject(context.Background(), CreateProjectRequest{Name: "Storefront", Slug: "storefront"})

	if err != nil {
		t.Fatal(err)
	}
	if project.ID != storefrontID {
		t.Fatalf("expected the created storefront project, got %+v", project)
	}
	requireRequestLines(t, stub, "POST /v1/projects")
}

func TestConnectionFailures_givesUpAfterThreeAttempts(t *testing.T) {
	stub, client := startStubAPI(t, resetConnection)

	_, err := client.GetProject(context.Background(), storefrontID)

	if err == nil {
		t.Fatal("expected the resets to fail the read")
	}
	requireRequestLines(t, stub, "GET "+storefrontPath, "GET "+storefrontPath, "GET "+storefrontPath)
}

func TestConnectionFailures_httpsToAPlainHTTPHostIsNotRepeated(t *testing.T) {
	stub := newStubAPI(t, answer(http.StatusOK, storefrontJSON))
	var connections atomic.Int32
	stub.server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	stub.server.Start()
	client := New("https://"+stub.server.Listener.Addr().String(), "connection-failure-test-token")

	_, err := client.GetProject(context.Background(), storefrontID)

	if err == nil {
		t.Fatal("expected HTTPS to a plain HTTP host to fail")
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("expected one connection, got %d", got)
	}
}
