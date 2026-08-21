package downloader

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	FormatCBZ    = "cbz"
	FormatFolder = "folder"
)

type PageData struct {
	Bytes     []byte
	Extension string
}

type ArchiveRequest struct {
	SourceID    string
	MangaTitle  string
	ChapterName string
	Format      string
	Pages       []PageData
	ComicInfo   ComicInfo
}

type Archiver struct {
	root string
}

func NewArchiver(root string) *Archiver {
	return &Archiver{root: root}
}

// Write packages a chapter in a temporary path and renames it only after all
// pages and metadata have been written successfully.
func (a *Archiver) Write(request ArchiveRequest) (string, error) {
	if len(request.Pages) == 0 {
		return "", errors.New("cannot archive a chapter without pages")
	}
	if request.Format == "" {
		request.Format = FormatCBZ
	}
	parent := filepath.Join(a.root, sanitizePathPart(request.SourceID), sanitizePathPart(request.MangaTitle))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("create archive directory: %w", err)
	}
	name := sanitizePathPart(request.ChapterName)
	if request.Format == FormatFolder {
		return a.writeFolder(parent, name, request)
	}
	if request.Format != FormatCBZ {
		return "", fmt.Errorf("unsupported archive format %q", request.Format)
	}
	return a.writeCBZ(parent, name, request)
}

func (a *Archiver) writeCBZ(parent, name string, request ArchiveRequest) (string, error) {
	finalPath := filepath.Join(parent, name+".cbz")
	temp, err := os.CreateTemp(parent, "."+name+".tmp-*.cbz")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		_ = temp.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	zw := zip.NewWriter(temp)
	for index, page := range request.Pages {
		entry, err := zw.Create(pageName(index, len(request.Pages), page.Extension))
		if err != nil {
			_ = zw.Close()
			return "", err
		}
		if _, err := entry.Write(page.Bytes); err != nil {
			_ = zw.Close()
			return "", err
		}
	}
	metadata, err := BuildComicInfo(request.ComicInfo)
	if err != nil {
		_ = zw.Close()
		return "", err
	}
	entry, err := zw.Create("ComicInfo.xml")
	if err != nil {
		_ = zw.Close()
		return "", err
	}
	if _, err := entry.Write(metadata); err != nil {
		_ = zw.Close()
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := replacePath(tempPath, finalPath, false); err != nil {
		return "", err
	}
	removeTemp = false
	return finalPath, nil
}

func (a *Archiver) writeFolder(parent, name string, request ArchiveRequest) (string, error) {
	finalPath := filepath.Join(parent, name)
	tempPath, err := os.MkdirTemp(parent, "."+name+".tmp-")
	if err != nil {
		return "", err
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.RemoveAll(tempPath)
		}
	}()

	for index, page := range request.Pages {
		path := filepath.Join(tempPath, pageName(index, len(request.Pages), page.Extension))
		if err := os.WriteFile(path, page.Bytes, 0o644); err != nil {
			return "", err
		}
	}
	metadata, err := BuildComicInfo(request.ComicInfo)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tempPath, "ComicInfo.xml"), metadata, 0o644); err != nil {
		return "", err
	}
	if err := replacePath(tempPath, finalPath, true); err != nil {
		return "", err
	}
	removeTemp = false
	return finalPath, nil
}

func replacePath(tempPath, finalPath string, directory bool) error {
	renameErr := os.Rename(tempPath, finalPath)
	if renameErr == nil {
		return nil
	}
	if _, err := os.Stat(finalPath); errors.Is(err, os.ErrNotExist) {
		return renameErr
	} else if err != nil {
		return err
	}
	var err error
	if directory {
		err = os.RemoveAll(finalPath)
	} else {
		err = os.Remove(finalPath)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	if err != nil {
		return err
	}
	return os.Rename(tempPath, finalPath)
}

func pageName(index, total int, extension string) string {
	width := len(fmt.Sprint(total))
	if width < 3 {
		width = 3
	}
	extension = strings.ToLower(strings.TrimSpace(extension))
	if extension == "" {
		extension = ".jpg"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	return fmt.Sprintf("%0*d%s", width, index+1, extension)
}

var unsafePathCharacters = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(unsafePathCharacters.ReplaceAllString(value, "_"))
	value = strings.TrimRight(value, ". ")
	if value == "" || value == "." || value == ".." {
		return "untitled"
	}
	return value
}
