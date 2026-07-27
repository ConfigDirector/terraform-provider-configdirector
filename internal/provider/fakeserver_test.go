package provider

import (
	"encoding/json"
	"fmt"
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
//
// environments is the single source of truth for a project's environments;
// projects never store their own Environments slice, it's always computed
// on read via projectEnvironmentsLocked so creates/updates/deletes through
// the environments endpoints stay consistent with what a project's
// "environments" summary reports.
type fakeConfigDirectorServer struct {
	mu               sync.Mutex
	projects         map[string]client.Project
	environments     map[string]client.Environment
	environmentOrder []string
	// configs is keyed by "projectID/key".
	configs        map[string]client.Config
	organizationID string
}

// newFakeConfigDirectorServer starts the fake server, registers cleanup, and
// returns its URL (to be set as CONFIGDIRECTOR_BASE_URL).
func newFakeConfigDirectorServer(t *testing.T) *httptest.Server {
	t.Helper()
	fake := &fakeConfigDirectorServer{
		projects:       make(map[string]client.Project),
		environments:   make(map[string]client.Environment),
		configs:        make(map[string]client.Config),
		organizationID: uuid.NewString(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/projects", fake.createProject)
	mux.HandleFunc("GET /v1/projects", fake.listProjects)
	mux.HandleFunc("GET /v1/projects/{id}", fake.getProject)
	mux.HandleFunc("PUT /v1/projects/{id}", fake.updateProject)
	mux.HandleFunc("DELETE /v1/projects/{id}", fake.deleteProject)

	mux.HandleFunc("POST /v1/projects/{projectId}/environments", fake.createEnvironment)
	mux.HandleFunc("GET /v1/projects/{projectId}/environments", fake.listEnvironments)
	mux.HandleFunc("GET /v1/projects/{projectId}/environments/{environmentId}", fake.getEnvironment)
	mux.HandleFunc("PUT /v1/projects/{projectId}/environments/{environmentId}", fake.updateEnvironment)
	mux.HandleFunc("DELETE /v1/projects/{projectId}/environments/{environmentId}", fake.deleteEnvironment)

	mux.HandleFunc("POST /v1/projects/{projectId}/configs", fake.createConfig)
	mux.HandleFunc("GET /v1/projects/{projectId}/configs", fake.listConfigs)
	mux.HandleFunc("GET /v1/projects/{projectId}/configs/{configKey}", fake.getConfig)
	mux.HandleFunc("PATCH /v1/projects/{projectId}/configs/{configKey}", fake.updateConfig)
	mux.HandleFunc("DELETE /v1/projects/{projectId}/configs/{configKey}", fake.deleteConfig)
	mux.HandleFunc("PUT /v1/projects/{projectId}/configs/{configKey}/targets", fake.updateConfigTargets)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// projectEnvironmentsLocked returns projectID's environments in creation
// order. Callers must hold f.mu.
func (f *fakeConfigDirectorServer) projectEnvironmentsLocked(projectID string) []client.Environment {
	var out []client.Environment
	for _, id := range f.environmentOrder {
		if e, ok := f.environments[id]; ok && e.ProjectID == projectID {
			out = append(out, e)
		}
	}
	return out
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
	}

	f.mu.Lock()
	f.projects[id] = p
	for _, e := range []client.Environment{
		{ID: uuid.NewString(), Slug: "test", Name: "Test", Color: "blue", ProjectID: id, Live: false},
		{ID: uuid.NewString(), Slug: "production", Name: "Production", Color: "green", ProjectID: id, Live: true},
	} {
		f.environments[e.ID] = e
		f.environmentOrder = append(f.environmentOrder, e.ID)
	}
	p.Environments = f.projectEnvironmentsLocked(id)
	f.mu.Unlock()

	writeJSON(w, http.StatusCreated, p)
}

func (f *fakeConfigDirectorServer) getProject(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	p, ok := f.projects[r.PathValue("id")]
	if ok {
		p.Environments = f.projectEnvironmentsLocked(p.ID)
	}
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
	id := r.PathValue("id")

	f.mu.Lock()
	delete(f.projects, id)
	for _, envID := range f.environmentOrder {
		if e, ok := f.environments[envID]; ok && e.ProjectID == id {
			delete(f.environments, envID)
		}
	}
	for key, cfg := range f.configs {
		if cfg.ProjectID == id {
			delete(f.configs, key)
		}
	}
	f.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeConfigDirectorServer) createEnvironment(w http.ResponseWriter, r *http.Request) {
	var req client.CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	projectID := r.PathValue("projectId")

	f.mu.Lock()
	_, ok := f.projects[projectID]
	var e client.Environment
	if ok {
		e = client.Environment{
			ID:        uuid.NewString(),
			Name:      req.Name,
			Slug:      req.Slug,
			Color:     req.Color,
			Live:      req.Live,
			ProjectID: projectID,
		}
		f.environments[e.ID] = e
		f.environmentOrder = append(f.environmentOrder, e.ID)
	}
	f.mu.Unlock()

	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (f *fakeConfigDirectorServer) getEnvironment(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	e, ok := f.environments[r.PathValue("environmentId")]
	f.mu.Unlock()
	if !ok || e.ProjectID != r.PathValue("projectId") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (f *fakeConfigDirectorServer) updateEnvironment(w http.ResponseWriter, r *http.Request) {
	var req client.UpdateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := r.PathValue("environmentId")

	f.mu.Lock()
	e, ok := f.environments[id]
	if ok && e.ProjectID == r.PathValue("projectId") {
		e.Name = req.Name
		e.Slug = req.Slug
		e.Color = req.Color
		e.Live = req.Live
		f.environments[id] = e
	} else {
		ok = false
	}
	f.mu.Unlock()

	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (f *fakeConfigDirectorServer) listEnvironments(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	out := f.projectEnvironmentsLocked(r.PathValue("projectId"))
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (f *fakeConfigDirectorServer) deleteEnvironment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("environmentId")

	f.mu.Lock()
	if e, ok := f.environments[id]; ok && e.ProjectID == r.PathValue("projectId") {
		delete(f.environments, id)
	}
	f.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// stringifyDefaultValue mirrors the real API's behavior of always storing
// and returning a target's default value as a string, regardless of what
// type was written (confirmed empirically against the live server).
func stringifyDefaultValue(v any) *string {
	if v == nil {
		return nil
	}
	s := fmt.Sprintf("%v", v)
	return &s
}

func (f *fakeConfigDirectorServer) createConfig(w http.ResponseWriter, r *http.Request) {
	var req client.CreateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	projectID := r.PathValue("projectId")

	f.mu.Lock()
	_, projectOK := f.projects[projectID]
	key := projectID + "/" + req.Key
	_, exists := f.configs[key]
	if !projectOK {
		f.mu.Unlock()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if exists {
		f.mu.Unlock()
		http.Error(w, "key already in use", http.StatusBadRequest)
		return
	}

	server, clientFlag := true, true
	if req.Server != nil {
		server = *req.Server
	}
	if req.Client != nil {
		clientFlag = *req.Client
	}
	variations := req.Variations
	if variations == nil {
		variations = []client.Variation{}
	}

	var targets []client.ConfigTarget
	for _, e := range f.projectEnvironmentsLocked(projectID) {
		targets = append(targets, client.ConfigTarget{
			ID:           uuid.NewString(),
			DefaultValue: stringifyDefaultValue(req.DefaultValue),
			Rules:        []any{},
			Environment:  client.ConfigTargetEnvironment{ID: e.ID, Name: e.Name, Slug: e.Slug},
		})
	}

	cfg := client.Config{
		ID:             uuid.NewString(),
		ProjectID:      projectID,
		Key:            req.Key,
		Description:    req.Description,
		Role:           req.Role,
		Lifetime:       req.Lifetime,
		Type:           req.Type,
		TypeOptions:    req.TypeOptions,
		State:          "new",
		Client:         clientFlag,
		Server:         server,
		Variations:     variations,
		Targets:        targets,
		DeprecatedKeys: []client.DeprecatedKey{},
	}
	f.configs[key] = cfg
	f.mu.Unlock()

	// The create response never includes targets (matches the real API).
	respCfg := cfg
	respCfg.Targets = nil
	writeJSON(w, http.StatusOK, respCfg)
}

func (f *fakeConfigDirectorServer) getConfig(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("projectId") + "/" + r.PathValue("configKey")

	f.mu.Lock()
	cfg, ok := f.configs[key]
	f.mu.Unlock()

	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// The single-config GET always includes targets (matches the real API).
	writeJSON(w, http.StatusOK, cfg)
}

func (f *fakeConfigDirectorServer) listConfigs(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")

	f.mu.Lock()
	var out []client.Config
	for _, cfg := range f.configs {
		if cfg.ProjectID == projectID {
			c := cfg
			c.Targets = nil
			out = append(out, c)
		}
	}
	f.mu.Unlock()

	writeJSON(w, http.StatusOK, out)
}

// fakeUpdateConfigRequest mirrors client's unexported updateConfigRequest
// (can't reference it directly - different package). The API's PATCH
// response body is empty on success (confirmed empirically, despite the
// OpenAPI spec documenting a body), so unlike other updates this handler
// doesn't return the updated config.
type fakeUpdateConfigRequest struct {
	Key          string             `json:"key"`
	Description  *string            `json:"description"`
	Role         string             `json:"role"`
	Lifetime     string             `json:"lifetime"`
	Type         string             `json:"type"`
	TypeOptions  any                `json:"typeOptions"`
	Variations   []client.Variation `json:"variations"`
	Availability *struct {
		Server bool `json:"server"`
		Client bool `json:"client"`
	} `json:"availability"`
}

func (f *fakeConfigDirectorServer) updateConfig(w http.ResponseWriter, r *http.Request) {
	var req fakeUpdateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	oldKey := r.PathValue("projectId") + "/" + r.PathValue("configKey")

	f.mu.Lock()
	cfg, ok := f.configs[oldKey]
	if ok {
		if req.Key != "" {
			cfg.Key = req.Key
		}
		if req.Description != nil {
			cfg.Description = req.Description
		}
		if req.Role != "" {
			cfg.Role = req.Role
		}
		if req.Lifetime != "" {
			cfg.Lifetime = req.Lifetime
		}
		if req.Type != "" {
			cfg.Type = req.Type
		}
		if req.TypeOptions != nil {
			cfg.TypeOptions = req.TypeOptions
		}
		if req.Variations != nil {
			cfg.Variations = req.Variations
		}
		if req.Availability != nil {
			cfg.Server = req.Availability.Server
			cfg.Client = req.Availability.Client
		}
		newKey := r.PathValue("projectId") + "/" + cfg.Key
		delete(f.configs, oldKey)
		f.configs[newKey] = cfg
	}
	f.mu.Unlock()

	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (f *fakeConfigDirectorServer) deleteConfig(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("projectId") + "/" + r.PathValue("configKey")

	f.mu.Lock()
	delete(f.configs, key)
	f.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

type fakeUpdateConfigTargetsRequest struct {
	EnvironmentID string `json:"environmentId"`
	DefaultValue  any    `json:"defaultValue"`
	Rules         any    `json:"rules"`
}

func (f *fakeConfigDirectorServer) updateConfigTargets(w http.ResponseWriter, r *http.Request) {
	var req fakeUpdateConfigTargetsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	key := r.PathValue("projectId") + "/" + r.PathValue("configKey")

	f.mu.Lock()
	cfg, ok := f.configs[key]
	if ok {
		found := false
		for i, t := range cfg.Targets {
			if t.Environment.ID == req.EnvironmentID {
				cfg.Targets[i].DefaultValue = stringifyDefaultValue(req.DefaultValue)
				cfg.Targets[i].Rules = req.Rules
				found = true
				break
			}
		}
		if found {
			f.configs[key] = cfg
		} else {
			ok = false
		}
	}
	f.mu.Unlock()

	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
