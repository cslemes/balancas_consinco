package mapper

import (
	"path/filepath"
	"regexp"
)

// FolderMapper maps remote SFTP folder names to local folder paths using regex pattern matching.
type FolderMapper struct {
	re *regexp.Regexp
}

// NewFolderMapper compiles the provided regex pattern or defaults to `(?i)^bal(\d+)$` if empty.
func NewFolderMapper(pattern string) (*FolderMapper, error) {
	if pattern == "" {
		pattern = `(?i)^bal(\d+)$`
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &FolderMapper{re: re}, nil
}

// MatchFolder checks if folderName matches the pattern. If matched, it returns the first capture group
// (or the full match if no capture group exists) and true. Otherwise, it returns ("", false).
func (m *FolderMapper) MatchFolder(folderName string) (string, bool) {
	matches := m.re.FindStringSubmatch(folderName)
	if len(matches) == 0 {
		return "", false
	}
	if len(matches) > 1 {
		return matches[1], true
	}
	return matches[0], true
}

// GetLocalFolder maps a remote folder name to a local path joined with baseDir.
// If the remote folder does not match the mapper pattern, it returns ("", false).
func (m *FolderMapper) GetLocalFolder(baseDir, remoteName string) (string, bool) {
	subName, ok := m.MatchFolder(remoteName)
	if !ok {
		return "", false
	}
	return filepath.Join(baseDir, subName), true
}
