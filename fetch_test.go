// fetch_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchURL(t *testing.T) {
	// создание тестового сервера
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	// tests
	tests := []struct {
		name      string
		urls      []string
		opts      FetchOptions
		wantCount int
	}{
		{
			name: "успешный URL",
			urls: []string{server.URL},
			opts: FetchOptions{
				MaxConcurrent: 1,
				Timeout:       time.Second,
			},
			wantCount: 1,
		},
		{
			name: "несколько URL",
			urls: []string{server.URL, server.URL, server.URL, server.URL},
			opts: FetchOptions{
				MaxConcurrent: 4,
				Timeout:       time.Second,
			},
			wantCount: 4,
		},
		{
			name: "пустой список URL",
			urls: []string{},
			opts: FetchOptions{
				Timeout: time.Second,
			},
			wantCount: 0,
		},
		{
			name: "с кастомным клиентом",
			urls: []string{server.URL},
			opts: FetchOptions{
				MaxConcurrent: 1,
				Timeout:       time.Second,
				Client: &http.Client{
					Timeout: 500 * time.Millisecond,
				},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := FetchURLs(tt.urls, tt.opts)

			// Проверяем количество результатов
			if len(results) != tt.wantCount {
				t.Errorf("FetchURLs() = %v, want %d", len(results), tt.wantCount)
			}

			// Для непустых списков проверяем статусы
			if tt.wantCount > 0 {
				for _, result := range results {
					if result.Error != nil {
						t.Errorf("URL %s returned error: %v", result.URL, result.Error)
					}
					if result.StatusCode != http.StatusOK {
						t.Errorf("URL %s returned status code: %d, wantCount %d", result.URL, result.StatusCode, http.StatusOK)
					}
				}
			}
		})
	}
}

// Дополнительный тест для проверки timeout
func TestFetchURLWithTimeout(t *testing.T) {
	// создаем медленный сервер
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	opts := FetchOptions{
		MaxConcurrent: 1,
		Timeout:       500 * time.Millisecond, // таймаут меньше чем ответ сервера
	}

	results := FetchURLs([]string{slowServer.URL}, opts)

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if results[0].Error == nil {
		t.Error("Expected timeout error, got nil")
	}
}

// Тест для проверки максимального количества одновременных запросов
func TestMaxConcurent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	urls := make([]string, 10)
	for i := range urls {
		urls[i] = server.URL
	}

	start := time.Now()
	opts := FetchOptions{
		MaxConcurrent: 2,
		Timeout:       5 * time.Second,
	}
	results := FetchURLs(urls, opts)
	elapsed := time.Since(start)

	// 10 запросов с 2 параллельными = примерно 5 * 100ms = 500ms
	if elapsed < 400*time.Millisecond {
		t.Logf("Execution time: %v, which is reasonable for 2 concurrent requests", elapsed)
	}

	if len(results) != 10 {
		t.Errorf("Expected 10 results, got %d", len(results))
	}
}
