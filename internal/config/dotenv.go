package config

import (
	"errors"
	"github.com/joho/godotenv"
	"os"
)

// LoadDotEnv loads an optional local file without replacing existing environment
// variables, including explicitly empty values. Missing files are normal on hosts
// such as Render. Parser errors are sanitized because they may contain secrets.
func LoadDotEnv(path string) error {
	if err := godotenv.Load(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.New("could not load .env; check file permissions and syntax")
	}
	return nil
}
