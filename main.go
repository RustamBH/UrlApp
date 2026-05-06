package main

import (
	"fmt"
	"time"
)

func main() {
	urls := []string{
		"https://yandex.ru",
		"https://google.com",
		"https://ozon.ru",
		"https://somesite.invalid", // несуществующий — для теста retry
		"https://yandex.ru",        // дубликат — будет deduplicated через singleflight
		"https://google.com",       // дубликат
		"https://yandex.ru",        // ещё один дубликат
		"https://httpstat.us/500",  // 500 ошибка — будет повторена
		"https://httpstat.us/404",  // 404 — не повторяется, но возвращается
	}

	// Настраиваем опции с агрессивным retry для демонстрации
	opts := DefaultOptions()
	opts.Retry.MaxTries = 2
	opts.Retry.InitialInterval = 200 * time.Millisecond
	results := FetchURLs(urls, opts)

	fmt.Println("\n=== Результаты ===")
	for i, res := range results {
		status := "OK"
		if res.Error != nil || res.StatusCode == -1 {
			status = "FAIL"
		}
		cacheMark := ""
		if res.Cached {
			cacheMark = " 🔄 (dedup)"
		}
		fmt.Printf("[%d] %s → %d (%s)%s\n", i, res.URL, res.StatusCode, status, cacheMark)
		if res.Error != nil {
			fmt.Printf("    Error: %v\n", res.Error)
		}
	}
}
