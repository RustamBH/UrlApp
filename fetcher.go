// fetcher.go
package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Result хранит результат запроса к одному URL
type Result struct {
	URL        string
	StatusCode int
	Error      error
}

// FetchOptions настройки параллельного фетчера
type FetchOptions struct {
	MaxConcurrent int
	Timeout       time.Duration
	Client        *http.Client
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
	}
}

// FetchURLs выполняет параллельные HTTP-запросы к указанным URL
func FetchURLs(urls []string, opts FetchOptions) []Result {
	if opts.Client == nil {
		opts = DefaultOptions()
	}

	results := make([]Result, len(urls))
	wg := &sync.WaitGroup{}
	sem := make(chan struct{}, opts.MaxConcurrent)

	for i, url := range urls {
		sem <- struct{}{}
		wg.Add(1)

		go func(idx int, u string) {
			defer wg.Done()
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				results[idx] = Result{URL: u, StatusCode: -1, Error: err}
				return
			}

			resp, err := opts.Client.Do(req)
			if err != nil {
				results[idx] = Result{URL: u, StatusCode: -1, Error: err}
				return
			}
			defer resp.Body.Close()

			results[idx] = Result{URL: u, StatusCode: resp.StatusCode, Error: nil}
		}(i, url)
	}

	wg.Wait()
	return results
}
