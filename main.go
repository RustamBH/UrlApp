// main.go
package main

import (
	"fmt"
)

func main() {
	urls := []string{
		"https://yandex.ru",
		"https://google.com",
		"https://ozon.ru",
		"https://somesite.com", // несуществующий — для теста ошибки
		"https://yandex.ru",
		"https://google.com",
		"https://yandex.ru",
		"https://dzen.ru/",
		"https://translate.yandex.ru/",
		"https://yandex.ru",
		"https://dzen.ru/",
		"https://111.ru/", // несуществующий — для теста ошибки
	}

	opts := DefaultOptions()
	results := FetchURLs(urls, opts)

	fmt.Println("\n=== Результаты ===")
	for i, res := range results {
		status := "OK"
		if res.Error != nil || res.StatusCode == -1 {
			status = "FAIL"
		}
		fmt.Printf("[%d] %s → %d (%s)\n", i, res.URL, res.StatusCode, status)
		if res.Error != nil {
			fmt.Printf("    Error: %v\n", res.Error)
		}
	}
}
