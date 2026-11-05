package engine

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPodmanServiceContainerInspect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/omnideck/json" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{
			"Created":"2026-08-13T20:00:00.123456789Z",
			"RestartCount":2,
			"State":{
				"Status":"running",
				"StartedAt":"2026-08-13T20:01:00.987654321Z",
				"Health":{"Status":"healthy"}
			}
		}`)
	}))
	defer server.Close()

	client := &podmanServiceClient{client: server.Client(), baseURL: server.URL}
	inspect, err := client.containerInspect("omnideck")
	if err != nil {
		t.Fatal(err)
	}
	if inspect.Status != "running" || inspect.RestartCount != 2 || inspect.HealthStatus != "healthy" {
		t.Fatalf("unexpected inspect data: %+v", inspect)
	}
	if want := time.Date(2026, 8, 13, 20, 0, 0, 123456789, time.UTC); !inspect.CreatedAt.Equal(want) {
		t.Fatalf("created = %s, want %s", inspect.CreatedAt, want)
	}
	if want := time.Date(2026, 8, 13, 20, 1, 0, 987654321, time.UTC); !inspect.StartedAt.Equal(want) {
		t.Fatalf("started = %s, want %s", inspect.StartedAt, want)
	}
}

func TestPodmanServiceContainerEnv(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/omnideck/json" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"State":{"Status":"running"},"Config":{"Env":["PORT=8080","QUERY=a=b|c","OMNIDECK_EXTERNAL_URL=http://localhost:2337"]}}`)
	}))
	defer server.Close()

	client := &podmanServiceClient{client: server.Client(), baseURL: server.URL}
	env, err := client.containerEnv("omnideck")
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := EnvValue(env, "OMNIDECK_EXTERNAL_URL"); !ok || value != "http://localhost:2337" {
		t.Fatalf("OMNIDECK_EXTERNAL_URL = %q, %v; env = %#v", value, ok, env)
	}
	if value, _ := EnvValue(env, "QUERY"); value != "a=b|c" {
		t.Fatalf("QUERY = %q", value)
	}
}

func TestPodmanServiceContainerImageID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/omnideck/json" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"Image":"sha256:4b1d","Config":{"Image":"ghcr.io/omnideck-dev/omnideck:latest"}}`)
	}))
	defer server.Close()

	client := &podmanServiceClient{client: server.Client(), baseURL: server.URL}
	id, err := client.containerImageID("omnideck")
	if err != nil || id != "sha256:4b1d" {
		t.Fatalf("containerImageID = %q, %v; want the image ID, not the tag", id, err)
	}
}

func TestPodmanServiceContainerStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0.0/libpod/containers/stats" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("containers") != "omnideck" || r.URL.Query().Get("stream") != "false" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"Error":null,"Stats":[{"CPU":0.1800404,"MemUsage":112783360,"MemLimit":2147483648,"MemPerc":5.251884}]}`)
	}))
	defer server.Close()

	client := &podmanServiceClient{client: server.Client(), baseURL: server.URL}
	cpu, cpuPct, ram, ramTotal, ramPct, err := client.containerStats("omnideck")
	if err != nil {
		t.Fatal(err)
	}
	if cpu != "0.18%" || math.Abs(cpuPct-0.001800404) > 1e-12 || ram != "107.6MiB" || ramTotal != "2GiB" || math.Abs(ramPct-0.05251884) > 1e-12 {
		t.Fatalf("unexpected stats: cpu=%q cpuPct=%v ram=%q total=%q ramPct=%v", cpu, cpuPct, ram, ramTotal, ramPct)
	}
}

func TestPodmanServiceReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"cause":"no such container"}`, http.StatusNotFound)
	}))
	defer server.Close()

	client := &podmanServiceClient{client: server.Client(), baseURL: server.URL}
	if _, err := client.containerInspect("missing"); err == nil {
		t.Fatal("expected service error")
	}
}
