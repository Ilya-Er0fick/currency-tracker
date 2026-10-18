package config

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// LoadEnvFile переносит настройки из файла в окружение процесса.
//
// Уже заданные переменные не перезаписываются: в контейнере файла нет,
// настройки приходят снаружи, и файл разработчика не должен их подменять.
// Отсутствие файла ошибкой не считается.
func LoadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func parseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}

	key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
	value = strings.TrimSpace(value)

	// Кавычки вокруг значения встречаются в примерах конфигураций,
	// но частью значения не являются.
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}

	if key == "" {
		return "", "", false
	}
	return key, value, true
}
