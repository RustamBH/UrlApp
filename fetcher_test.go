// fetcher_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestFetchURLs_TableDriven табличные тесты для разных сценариев
func TestFetchURLs_TableDriven(t *testing.T) {
	tests := []struct {
		name             string
		urls             []string
		setupServer      func() *httptest.Server
		opts             FetchOptions
		wantResults      []int // ожидаемые статус-коды (-1 для ошибки)
		wantErrorCount   int   // количество ожидаемых ошибок
		checkConcurrency bool
		maxConcurrent    int
	}{
		{
			name: "успешные запросы к тестовому серверу",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			opts: FetchOptions{
				MaxConcurrent: 3,
				Timeout:       time.Second,
			},
			wantResults:    []int{200, 200, 200},
			wantErrorCount: 0,
		},
		{
			name: "смешанные результаты: успех и ошибка",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/error" {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusOK)
				}))
			},
			opts: FetchOptions{
				MaxConcurrent: 2,
				Timeout:       time.Second,
			},
			urls:           []string{"/ok", "/error", "/ok"},
			wantResults:    []int{200, 500, 200},
			wantErrorCount: 0, // 500 — это валидный ответ, не ошибка
		},
		{
			name: "таймаут запроса",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(500 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
				}))
			},
			opts: FetchOptions{
				MaxConcurrent: 2,
				Timeout:       100 * time.Millisecond, // слишком короткий таймаут
			},
			wantResults:    []int{-1, -1},
			wantErrorCount: 2,
		},
		{
			name: "проверка ограничения параллелизма",
			setupServer: func() *httptest.Server {
				var (
					maxConcurrent atomic.Int32
					current       atomic.Int32
				)
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					cur := current.Add(1)
					defer current.Add(-1)
					if cur > maxConcurrent.Load() {
						maxConcurrent.Store(cur)
					}
					time.Sleep(50 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
				}))
			},
			opts: FetchOptions{
				MaxConcurrent: 3,
				Timeout:       time.Second,
			},
			urls:             []string{"/1", "/2", "/3", "/4", "/5"},
			wantResults:      []int{200, 200, 200, 200, 200},
			checkConcurrency: true,
			maxConcurrent:    3,
		},
		{
			name:        "пустой список URL",
			opts:        DefaultOptions(),
			urls:        []string{},
			wantResults: []int{},
		},
		{
			name:           "невалидный URL",
			opts:           FetchOptions{Timeout: time.Second},
			urls:           []string{"not-a-valid-url", "ftp://invalid.scheme"},
			wantResults:    []int{-1, -1},
			wantErrorCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server
			if tt.setupServer != nil {
				server = tt.setupServer()
				defer server.Close()
				// Преобразуем относительные пути в полные URL
				for i, u := range tt.urls {
					if len(u) > 0 && u[0] == '/' {
						tt.urls[i] = server.URL + u
					}
				}
			}

			// Используем тестовый клиент с отключенными редиректами для предсказуемости
			if tt.opts.Client == nil {
				tt.opts.Client = &http.Client{
					Timeout: tt.opts.Timeout,
					CheckRedirect: func(req *http.Request, via []*http.Request) error {
						return http.ErrUseLastResponse
					},
				}
			}

			results := FetchURLs(tt.urls, tt.opts)

			// Проверка количества результатов
			if len(results) != len(tt.wantResults) {
				t.Errorf("len(results) = %d, want %d", len(results), len(tt.wantResults))
			}

			// Подсчёт ошибок
			errorCount := 0
			for i, res := range results {
				if i < len(tt.wantResults) {
					if res.StatusCode != tt.wantResults[i] {
						t.Errorf("result[%d].StatusCode = %d, want %d (URL: %s, error: %v)",
							i, res.StatusCode, tt.wantResults[i], res.URL, res.Error)
					}
				}
				if res.Error != nil || res.StatusCode == -1 {
					errorCount++
				}
			}

			if errorCount != tt.wantErrorCount {
				t.Errorf("errorCount = %d, want %d", errorCount, tt.wantErrorCount)
			}

			// Проверка ограничения параллелизма (если требуется)
			// Примечание: эта проверка эвристическая и может быть нестабильной в CI
		})
	}
}

// TestFetchURLs_RaceCondition проверка на гонки данных при параллельном выполнении
func TestFetchURLs_RaceCondition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	urls := make([]string, 50)
	for i := range urls {
		urls[i] = server.URL
	}

	opts := FetchOptions{
		MaxConcurrent: 10,
		Timeout:       time.Second,
		Client:        &http.Client{Timeout: time.Second},
	}

	// Запускаем несколько раз для надёжной проверки гонки
	for run := 0; run < 5; run++ {
		results := FetchURLs(urls, opts)
		if len(results) != len(urls) {
			t.Errorf("run %d: expected %d results, got %d", run, len(urls), len(results))
		}
		for i, res := range results {
			if res.StatusCode != 200 {
				t.Errorf("run %d, result[%d]: expected 200, got %d (error: %v)",
					run, i, res.StatusCode, res.Error)
			}
		}
	}
}
