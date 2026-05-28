package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type app struct {
	mu         sync.Mutex
	nextJobID  int
	nextWorkID int
	jobs       map[string]*job
	workers    map[string]*worker
}

type job struct {
	ID      string            `json:"id"`
	Module  string            `json:"module"`
	Version string            `json:"version"`
	Matrix  map[string]string `json:"matrix,omitempty"`
	Events  []eventRequest    `json:"events,omitempty"`

	signal chan signalResponse
}

type worker struct {
	ID        string `json:"id"`
	Token     string `json:"-"`
	JobID     string `json:"jobID"`
	Connected bool   `json:"connected"`
}

type createJobRequest struct {
	Module  string            `json:"module"`
	Version string            `json:"version"`
	Matrix  map[string]string `json:"matrix,omitempty"`
}

type createJobResponse struct {
	JobID       string `json:"jobID"`
	WorkerID    string `json:"workerID"`
	WorkerToken string `json:"workerToken"`
}

type signalRequest struct {
	Command string `json:"command"`
}

type signalResponse struct {
	Command string `json:"command"`
	JobID   string `json:"jobID"`
}

type eventRequest struct {
	Type   string `json:"type"`
	Status string `json:"status,omitempty"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

func newApp() *app {
	return &app{
		jobs:    map[string]*job{},
		workers: map[string]*worker{},
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs", a.createJob)
	mux.HandleFunc("GET /jobs/{jobID}", a.getJob)
	mux.HandleFunc("POST /jobs/{jobID}/signal", a.signalJob)
	mux.HandleFunc("GET /workers/{workerID}/signal", a.waitSignal)
	mux.HandleFunc("POST /jobs/{jobID}/events", a.postEvent)
	return mux
}

func (a *app) createJob(w http.ResponseWriter, r *http.Request) {
	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	a.nextJobID++
	a.nextWorkID++
	jobID := fmt.Sprintf("job-%d", a.nextJobID)
	workerID := fmt.Sprintf("worker-%d", a.nextWorkID)
	workerToken := fmt.Sprintf("demo-token-%d", a.nextWorkID)
	a.jobs[jobID] = &job{
		ID:      jobID,
		Module:  req.Module,
		Version: req.Version,
		Matrix:  req.Matrix,
		signal:  make(chan signalResponse, 1),
	}
	a.workers[workerID] = &worker{
		ID:    workerID,
		Token: workerToken,
		JobID: jobID,
	}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, createJobResponse{
		JobID:       jobID,
		WorkerID:    workerID,
		WorkerToken: workerToken,
	})
}

func (a *app) getJob(w http.ResponseWriter, r *http.Request) {
	j := a.jobByID(w, r.PathValue("jobID"))
	if j == nil {
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (a *app) signalJob(w http.ResponseWriter, r *http.Request) {
	j := a.jobByID(w, r.PathValue("jobID"))
	if j == nil {
		return
	}
	var req signalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	select {
	case j.signal <- signalResponse{Command: req.Command, JobID: j.ID}:
		writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	default:
		http.Error(w, "signal already pending", http.StatusConflict)
	}
}

func (a *app) waitSignal(w http.ResponseWriter, r *http.Request) {
	wrk := a.workerByID(w, r.PathValue("workerID"))
	if wrk == nil {
		return
	}
	if got, want := r.Header.Get("Authorization"), "Bearer "+wrk.Token; got != want {
		http.Error(w, "invalid worker token", http.StatusUnauthorized)
		return
	}
	a.mu.Lock()
	wrk.Connected = true
	j := a.jobs[wrk.JobID]
	a.mu.Unlock()
	if j == nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	timeout := 30 * time.Second
	if raw := r.URL.Query().Get("timeout"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	select {
	case signal := <-j.signal:
		writeJSON(w, http.StatusOK, signal)
	case <-time.After(timeout):
		http.Error(w, "timeout waiting for signal", http.StatusGatewayTimeout)
	}
}

func (a *app) workerByID(w http.ResponseWriter, id string) *worker {
	a.mu.Lock()
	wrk := a.workers[id]
	a.mu.Unlock()
	if wrk == nil {
		http.Error(w, "worker not found", http.StatusNotFound)
		return nil
	}
	return wrk
}

func (a *app) postEvent(w http.ResponseWriter, r *http.Request) {
	j := a.jobByID(w, r.PathValue("jobID"))
	if j == nil {
		return
	}
	var req eventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	j.Events = append(j.Events, req)
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (a *app) jobByID(w http.ResponseWriter, id string) *job {
	a.mu.Lock()
	j := a.jobs[id]
	a.mu.Unlock()
	if j == nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return nil
	}
	return j
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()
	log.Printf("cloud-build demo server listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, newApp().routes()))
}
