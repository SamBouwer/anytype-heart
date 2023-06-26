package source

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
)

type Zip struct{}

func NewZip() *Zip {
	return &Zip{}
}

func (d *Zip) GetFileReaders(importPath string, expectedExt []string) (map[string]io.ReadCloser, error) {
	r, err := zip.OpenReader(importPath)
	if err != nil {
		return nil, err
	}
	files := make(map[string]io.ReadCloser, 0)
	zipName := strings.TrimSuffix(importPath, filepath.Ext(importPath))
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		if f.FileInfo() != nil && f.FileInfo().IsDir() {
			dir := NewDirectory()
			fr, e := dir.GetFileReaders(f.Name, expectedExt)
			if e != nil {
				log.Errorf("failed to get files from directory, %s", e)
			}
			files = lo.Assign(files, fr)
			continue
		}
		ext := filepath.Ext(f.Name)
		if !isSupportedExtension(ext, expectedExt) {
			log.Errorf("not expected extension")
			continue
		}
		resultPath := filepath.Clean(f.Name)
		// remove zip root folder if exists
		resultPath = zipName + string(filepath.Separator) + resultPath
		rc, err := f.Open()
		if err != nil {
			log.Errorf("failed to read file: %s", err.Error())
			continue
		}
		files[resultPath] = rc
	}
	return files, nil
}
