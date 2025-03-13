package graphql

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler/lru"
)

func BuildPersistedQueryCache() (graphql.Cache[string], error) {
	filepath := "./persisted_queries.json"
	size, err := countLines(filepath)
	if err != nil {
		return nil, fmt.Errorf("getting size for cache: %w", err)
	}

	cache := lru.New[string](size + 1000)

	err = preloadCacheFromJSON(cache, "./persisted_queries.json")
	if err != nil {
		return nil, fmt.Errorf("preloading cache: %v", err)
	}

	return cache, nil
}

func loadJSONData(filePath string) (map[string]any, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func preloadCacheFromJSON(cache graphql.Cache[string], filePath string) error {
	data, err := loadJSONData(filePath)
	if err != nil {
		return err
	}

	ctx := context.Background()
	for key, value := range data {
		strValue, ok := value.(string)
		if !ok {
			return fmt.Errorf("invalid value type for key %s: expected string, got %T", key, value)
		}
		cache.Add(ctx, key, strValue)
	}
	return nil
}

func countLines(filePath string) (int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("reading file: %w", err)
	}

	return lineCount, nil
}
