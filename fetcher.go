// fetcher.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"golang.org/x/sync/singleflight"
)

// Result хранит результат запроса к одному URL
type Result struct {
	URL        string
	StatusCode int
	Error      error
	Cached     bool // true, если результат получен через singleflight от другого запроса
}

// RetryConfig конфигурация retry-механизма
type RetryConfig struct {
	MaxTries        uint
	MaxElapsedTime  time.Duration
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	RandomizeFactor float64
	ShouldRetry     func(error) bool
}

// DefaultRetryConfig значения по умолчанию для retry
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxTries:        3,
		MaxElapsedTime:  10 * time.Second,
		InitialInterval: 500 * time.Millisecond,
		MaxInterval:     5 * time.Second,
		Multiplier:      2.0,
		RandomizeFactor: 0.25,
		ShouldRetry: func(err error) bool {
			return !isPermanentError(err)
		},
	}
}

// isPermanentError определяет, стоит ли повторять запрос при данной ошибке
func isPermanentError(err error) bool {
	if err == nil {
		return false
	}
	// Контекст отменён — не повторяем
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	// Ошибки типа "неверный URL", "неподдерживаемый протокол" — не повторяем
	// backoff.Permanent оборачивает такие ошибки
	return false
}

// FetchOptions общие настройки фетчера
type FetchOptions struct {
	MaxConcurrent int
	Timeout       time.Duration
	Client        *http.Client
	Retry         RetryConfig
}

// DefaultOptions значения по умолчанию
func DefaultOptions() FetchOptions {
	return FetchOptions{
		MaxConcurrent: 5,
		Timeout:       2 * time.Second,
		Client: &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		Retry: DefaultRetryConfig(),
	}
}

// fetchWithRetry выполняет один HTTP-запрос с retry-логикой
func fetchWithRetry(ctx context.Context, client *http.Client, url string, cfg RetryConfig) (*http.Response, error) {
	// Создаём exponential backoff
	eb := backoff.NewExponentialBackOff()
	eb.InitialInterval = cfg.InitialInterval
	eb.RandomizationFactor = cfg.RandomizeFactor
	eb.Multiplier = cfg.Multiplier
	eb.MaxInterval = cfg.MaxInterval
	eb.Reset()

	var lastErr error
	for attempt := uint(1); attempt <= cfg.MaxTries; attempt++ {
		// Проверяем контекст перед каждой попыткой
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			// Ошибка создания запроса — не повторяем
			return nil, backoff.Permanent(err)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			// Проверяем, стоит ли повторять
			if cfg.ShouldRetry != nil && !cfg.ShouldRetry(err) {
				return nil, backoff.Permanent(err)
			}
			// Ждём перед следующей попыткой
			next := eb.NextBackOff()
			if next == backoff.Stop {
				break
			}
			timer := time.NewTimer(next)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
				// продолжаем цикл
			}
			continue
		}

		// 5xx — серверная ошибка, повторяем (но закрываем тело, чтобы не утекали ресурсы)
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			resp.Body.Close()
			lastErr = fmt.Errorf("server error %d", resp.StatusCode)
			next := eb.NextBackOff()
			if next == backoff.Stop {
				return nil, lastErr
			}
			timer := time.NewTimer(next)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
				continue
			}
		}

		// 4xx и 2xx/3xx — возвращаем как есть
		return resp, nil
	}

	return nil, lastErr
}

// FetchURLs выполняет параллельные запросы с дедупликацией через singleflight
func FetchURLs(urls []string, opts FetchOptions) []Result {
	if opts.Client == nil {
		opts = DefaultOptions()
	}

	results := make([]Result, len(urls))
	var sf singleflight.Group
	var wg sync.WaitGroup
	sem := make(chan struct{}, opts.MaxConcurrent)

	for i, url := range urls {
		sem <- struct{}{} // acquire semaphore
		wg.Add(1)

		go func(idx int, u string) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore

			// singleflight.Do гарантирует, что одинаковые URL выполнятся один раз
			// shared == true, если этот запрос "подключился" к уже выполняемому
			v, err, shared := sf.Do(u, func() (any, error) {
				ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
				defer cancel()

				resp, fetchErr := fetchWithRetry(ctx, opts.Client, u, opts.Retry)
				if fetchErr != nil {
					return Result{URL: u, StatusCode: -1, Error: fetchErr}, nil
				}
				defer resp.Body.Close()

				return Result{URL: u, StatusCode: resp.StatusCode, Error: nil}, nil
			})

			if err != nil {
				results[idx] = Result{URL: u, StatusCode: -1, Error: err, Cached: shared}
				return
			}

			if res, ok := v.(Result); ok {
				results[idx] = Result{
					URL:        res.URL,
					StatusCode: res.StatusCode,
					Error:      res.Error,
					Cached:     shared,
				}
			} else {
				results[idx] = Result{
					URL:        u,
					StatusCode: -1,
					Error:      fmt.Errorf("unexpected result type: %T", v),
					Cached:     shared,
				}
			}
		}(i, url)
	}

	wg.Wait()
	return results
}
