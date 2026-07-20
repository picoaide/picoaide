package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	cfg := Config{
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-test",
		Model:   "gpt-4o-mini",
	}
	client := NewClient(cfg)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestChat(t *testing.T) {
	var reqBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q, want Bearer sk-test", got)
		}
		defer r.Body.Close()
		json.NewDecoder(r.Body).Decode(&reqBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hello!"}}]}`))
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-4o-mini"})
	resp, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
	if reqBody["model"] != "gpt-4o-mini" {
		t.Errorf("model = %v, want gpt-4o-mini", reqBody["model"])
	}
}

func TestChat_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL, APIKey: "sk-bad", Model: "gpt-4o-mini"})
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestChat_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Late!"}}]}`))
	}))
	defer srv.Close()

	client := NewClient(Config{
		BaseURL: srv.URL,
		APIKey:  "sk-test",
		Model:   "gpt-4o-mini",
		Timeout: 10 * time.Millisecond,
	})
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
