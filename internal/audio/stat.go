package audio

import (
	"os"
)

func defaultOsStat(path string) (fileInfo, error) {
	return os.Stat(path)
}
