package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWorkerReceivesSignal(t *testing.T) {
	app := newApp()
	server := httptest.NewServer(app.routes())
	defer server.Close()

	created := createJob(t, server.URL)

	result := make(chan signalResponse, 1)
	go func() {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/workers/"+created.WorkerID+"/signal?timeout=2s", nil)
		if err != nil {
			t.Errorf("create signal request: %v", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+created.WorkerToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("signal request failed: %v", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("signal status = %d, want %d", resp.StatusCode, http.StatusOK)
			return
		}
		var got signalResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Errorf("decode signal: %v", err)
			return
		}
		result <- got
	}()

	postJSON(t, server.URL+"/jobs/"+created.JobID+"/signal", signalRequest{
		Command: "./llar make -v madler/zlib@v1.3.1",
	})

	select {
	case got := <-result:
		if got.Command != "./llar make -v madler/zlib@v1.3.1" {
			t.Fatalf("command = %q", got.Command)
		}
		if got.JobID != created.JobID {
			t.Fatalf("jobID = %q, want %q", got.JobID, created.JobID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not receive signal")
	}
}

func TestWorkerPostsEvent(t *testing.T) {
	app := newApp()
	server := httptest.NewServer(app.routes())
	defer server.Close()

	created := createJob(t, server.URL)
	postJSON(t, server.URL+"/jobs/"+created.JobID+"/events", eventRequest{
		Type:   "status",
		Status: "completed",
		Output: "ok",
	})

	resp, err := http.Get(server.URL + "/jobs/" + created.JobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	defer resp.Body.Close()
	var got job
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if len(got.Events) != 1 || got.Events[0].Status != "completed" {
		t.Fatalf("events = %+v", got.Events)
	}
}

func createJob(t *testing.T, baseURL string) createJobResponse {
	t.Helper()
	resp := postJSON(t, baseURL+"/jobs", createJobRequest{
		Module:  "madler/zlib",
		Version: "v1.3.1",
		Matrix:  map[string]string{"arch": "amd64", "os": "linux"},
	})
	var got createJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode create job: %v", err)
	}
	resp.Body.Close()
	if got.JobID == "" || got.WorkerID == "" || got.WorkerToken == "" {
		t.Fatalf("incomplete create job response: %+v", got)
	}
	return got
}

func postJSON[T any](t *testing.T, url string, body T) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("post %s status = %d", url, resp.StatusCode)
	}
	return resp
}
