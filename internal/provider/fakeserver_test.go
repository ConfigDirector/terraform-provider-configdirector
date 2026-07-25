package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/alejandro/terraform-provider-configdirector/internal/client"
)

// fakeConfigDirectorServer is a minimal in-memory stand-in for the
// ConfigDirector API. Pointing the provider at it (via
// CONFIGDIRECTOR_BASE_URL) lets acceptance tests exercise the full
// create/read/update/import lifecycle hermetically, without live credentials
// or network access.
//
// Only the endpoints exercised by tests so far are implemented; add
// resource-specific handlers here as coverage grows rather than duplicating
// a server per resource test file.
type fakeConfigDirectorServer struct {
	mu             sync.Mutex
	projects       map[string]client.Project
	organizationID string
}

// newFakeConfigDirectorServer starts the fake server, registers cleanup, and
// returns its URL (to be set as CONFIGDIRECTOR_BASE_URL).
func newFakeConfigDirectorServer(t *testing.T) *httptest.Server {
	t.Helper()
	fake := &fakeConfigDirectorServer{
		projects:       make(map[string]client.Project),
		organizationID: uuid.NewString(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/projects", fake.createProject)
	mux.HandleFunc("GET /v1/projects", fake.listProjects)
	mux.HandleFunc("GET /v1/projects/{id}", fake.getProject)
	mux.HandleFunc("PUT /v1/projects/{id}", fake.updateProject)
	mux.HandleFunc("DELETE /v1/projects/{id}", fake.deleteProject)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeConfigDirectorServer) createProject(w http.ResponseWriter, r *http.Request) {
	var req client.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := uuid.NewString()
	p := client.Project{
		ID:             id,
		Name:           req.Name,
		Slug:           req.Slug,
		OrganizationID: f.organizationID,
		Environments: []client.Environment{
			{ID: uuid.NewString(), Slug: "test", Name: "Test", Color: "blue", ProjectID: id, Live: false},
			{ID: uuid.NewString(), Slug: "production", Name: "Production", Color: "green", ProjectID: id, Live: true},
		},
	}

	f.mu.Lock()
	f.projects[id] = p
	f.mu.Unlock()

	writeJSON(w, http.StatusCreated, p)
}

func (f *fakeConfigDirectorServer) getProject(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	p, ok := f.projects[r.PathValue("id")]
	f.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (f *fakeConfigDirectorServer) updateProject(w http.ResponseWriter, r *http.Request) {
	var req client.UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := r.PathValue("id")

	f.mu.Lock()
	p, ok := f.projects[id]
	if ok {
		p.Name = req.Name
		p.Slug = req.Slug
		f.projects[id] = p
	}
	f.mu.Unlock()

	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeConfigDirectorServer) listProjects(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	out := make([]client.Project, 0, len(f.projects))
	for _, p := range f.projects {
		out = append(out, p)
	}
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (f *fakeConfigDirectorServer) deleteProject(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	delete(f.projects, r.PathValue("id"))
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
