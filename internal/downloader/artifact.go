package downloader

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const comicInfoEntry = "ComicInfo.xml"

// ArtifactImageEntries lists the page entry names of a download artifact in
// page order. Archives and extracted folders share the archiver's zero-padded
// page naming, so a lexicographic sort restores the page order.
func ArtifactImageEntries(artifactPath string) ([]string, error) {
	info, err := os.Stat(artifactPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(artifactPath)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == comicInfoEntry {
				continue
			}
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		return names, nil
	}
	reader, err := zip.OpenReader(artifactPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || file.Name == comicInfoEntry {
			continue
		}
		names = append(names, file.Name)
	}
	sort.Strings(names)
	return names, nil
}

// ReadArtifactPage returns the processed image bytes stored at the given
// zero-based page position of a download artifact.
func ReadArtifactPage(artifactPath string, index int) ([]byte, error) {
	if index < 0 {
		return nil, errors.New("page index is negative")
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		names, err := ArtifactImageEntries(artifactPath)
		if err != nil {
			return nil, err
		}
		if index >= len(names) {
			return nil, errors.New("page index is beyond the artifact")
		}
		return os.ReadFile(filepath.Join(artifactPath, names[index]))
	}
	reader, err := zip.OpenReader(artifactPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		if !file.FileInfo().IsDir() && file.Name != comicInfoEntry {
			names = append(names, file.Name)
		}
	}
	sort.Strings(names)
	if index >= len(names) {
		return nil, errors.New("page index is beyond the artifact")
	}
	target := names[index]
	for _, file := range reader.File {
		if file.Name != target {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = opened.Close() }()
		return io.ReadAll(opened)
	}
	return nil, errors.New("artifact entry vanished while reading")
}
