package markdown

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/globalsign/mgo/bson"

	ce "github.com/anyproto/anytype-heart/core/block/import/converter"
	"github.com/anyproto/anytype-heart/core/block/import/markdown/anymark"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/util/uri"
)

type Service interface {
	TempDir() string
}

type mdConverter struct {
	Service
}

type FileInfo struct {
	os.FileInfo
	io.ReadCloser
	HasInboundLinks bool
	PageID          string
	IsRootFile      bool
	Title           string
	ParsedBlocks    []*model.Block
	Source          string
}

func newMDConverter(s Service) *mdConverter {
	return &mdConverter{Service: s}
}

func (m *mdConverter) markdownToBlocks(importPath string, mode string) (map[string]*FileInfo, ce.ConvertError) {
	allErrors := ce.NewError()
	files := m.processFiles(importPath, mode, allErrors)

	log.Debug("2. DirWithMarkdownToBlocks: MarkdownToBlocks completed")

	return files, allErrors
}

func (m *mdConverter) processFiles(importPath string, mode string, allErrors ce.ConvertError) map[string]*FileInfo {
	fileInfo := make(map[string]*FileInfo, 0)
	ext := filepath.Ext(importPath)
	if strings.EqualFold(ext, ".zip") {
		fileInfo = m.processZipFile(importPath, mode, allErrors)
	} else if strings.EqualFold(ext, ".md") {
		m.processFile(importPath, fileInfo, allErrors)
	} else {
		fileInfo = m.processDirectory(importPath, allErrors)
	}
	for _, file := range fileInfo {
		for _, b := range file.ParsedBlocks {
			m.processFileBlock(b, fileInfo)
		}
	}
	return fileInfo
}

func (m *mdConverter) processZipFile(importPath, mode string, allErrors ce.ConvertError) map[string]*FileInfo {
	r, err := zip.OpenReader(importPath)
	if err != nil {
		allErrors.Add(importPath, err)
		return nil
	}
	defer r.Close()
	files := make(map[string]*FileInfo, 0)
	zipName := strings.TrimSuffix(importPath, filepath.Ext(importPath))
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		shortPath := filepath.Clean(f.Name)
		// remove zip root folder if exists
		shortPath = strings.TrimPrefix(shortPath, zipName+"/")

		rc, err := f.Open()
		if err != nil {
			allErrors.Add(shortPath, err)
			if mode == pb.RpcObjectImportRequest_ALL_OR_NOTHING.String() {
				return nil
			}
			log.Errorf("failed to read file %s: %s", shortPath, err.Error())
			continue
		}
		files[shortPath] = &FileInfo{}
		files[shortPath].Source = ce.GetSourceDetail(shortPath, importPath)
		if err := m.createBlocksFromFile(shortPath, rc, files); err != nil {
			allErrors.Add(shortPath, err)
			if mode == pb.RpcObjectImportRequest_ALL_OR_NOTHING.String() {
				return nil
			}
			log.Errorf("failed to create blocks from file %s: %s", shortPath, err.Error())
		}
	}

	return files
}

func (m *mdConverter) processDirectory(importPath string, allErrors ce.ConvertError) map[string]*FileInfo {
	files := make(map[string]*FileInfo)
	err := filepath.Walk(importPath,
		func(path string, info os.FileInfo, err error) error {
			if info != nil && !info.IsDir() {
				shortPath, err := filepath.Rel(importPath+string(filepath.Separator), path)
				if err != nil {
					return fmt.Errorf("failed to get relative path %s", err)
				}
				f, err := os.Open(path)
				if err != nil {
					return fmt.Errorf("failed to open file: %s", err)
				}
				files[shortPath] = &FileInfo{}
				if err = m.createBlocksFromFile(shortPath, f, files); err != nil {
					log.Errorf("failed to create blocks from file %s: %s", shortPath, err)
				}
				files[shortPath].Source = ce.GetSourceDetail(shortPath, importPath)
			}

			return nil
		},
	)
	for _, file := range files {
		for _, b := range file.ParsedBlocks {
			m.processFileBlock(b, files)
		}
	}
	if err != nil {
		allErrors.Add(importPath, err)
	}
	return files
}

func (m *mdConverter) processBlocks(shortPath string, file *FileInfo, files map[string]*FileInfo) {
	for _, block := range file.ParsedBlocks {
		m.processTextBlock(block, files)
	}
	m.processLinkBlock(shortPath, file, files)
}

func (m *mdConverter) processTextBlock(block *model.Block, files map[string]*FileInfo) {
	txt := block.GetText()
	if txt != nil && txt.Marks != nil && len(txt.Marks.Marks) == 1 &&
		txt.Marks.Marks[0].Type == model.BlockContentTextMark_Link {

		link := txt.Marks.Marks[0].Param

		var wholeLineLink bool
		textRunes := []rune(txt.Text)
		var from, to = int(txt.Marks.Marks[0].Range.From), int(txt.Marks.Marks[0].Range.To)
		if from == 0 || (from < len(textRunes) && len(strings.TrimSpace(string(textRunes[0:from]))) == 0) {
			if to >= len(textRunes) || len(strings.TrimSpace(string(textRunes[to:]))) == 0 {
				wholeLineLink = true
			}
		}

		ext := filepath.Ext(link)

		// todo: bug with multiple markup links in arow when the first is external
		if file := files[link]; file != nil {
			if strings.EqualFold(ext, ".md") {
				// only convert if this is the only link in the row
				if wholeLineLink {
					m.convertTextToPageLink(block)
				} else {
					m.convertTextToPageMention(block)
				}
			} else {
				m.convertTextToFile(block)
			}

			if strings.EqualFold(ext, ".csv") {
				csvDir := strings.TrimSuffix(link, ext)
				for name, file := range files {
					// set HasInboundLinks for all CSV-origin md files
					fileExt := filepath.Ext(name)
					if filepath.Dir(name) == csvDir && strings.EqualFold(fileExt, ".md") {
						file.HasInboundLinks = true
					}
				}
			}
			file.HasInboundLinks = true
		} else if wholeLineLink {
			m.convertTextToBookmark(block)
		} else {
			log.Debugf("")
		}
	}
}

func (m *mdConverter) processFileBlock(block *model.Block, files map[string]*FileInfo) {
	if f := block.GetFile(); f != nil {
		if block.Id == "" {
			block.Id = bson.NewObjectId().Hex()
		}
		m.createFile(f, block.Id, files)
	}
}

func (m *mdConverter) processLinkBlock(shortPath string, file *FileInfo, files map[string]*FileInfo) {
	ext := filepath.Ext(shortPath)
	dependentFilesDir := strings.TrimSuffix(shortPath, ext)
	var hasUnlinkedDependentMDFiles bool
	for targetName, targetFile := range files {
		fileExt := filepath.Ext(targetName)
		if filepath.Dir(targetName) == dependentFilesDir && strings.EqualFold(fileExt, ".md") {
			if !targetFile.HasInboundLinks {
				if !hasUnlinkedDependentMDFiles {
					// add Unsorted header
					file.ParsedBlocks = append(file.ParsedBlocks, &model.Block{
						Id: bson.NewObjectId().Hex(),
						Content: &model.BlockContentOfText{Text: &model.BlockContentText{
							Text:  "Unsorted",
							Style: model.BlockContentText_Header3,
						}},
					})
					hasUnlinkedDependentMDFiles = true
				}

				file.ParsedBlocks = append(file.ParsedBlocks, &model.Block{
					Id: bson.NewObjectId().Hex(),
					Content: &model.BlockContentOfLink{Link: &model.BlockContentLink{
						TargetBlockId: targetName,
						Style:         model.BlockContentLink_Page,
					}},
				})
				targetFile.HasInboundLinks = true
			}
		}
	}
}

func (m *mdConverter) convertTextToPageLink(block *model.Block) {
	block.Content = &model.BlockContentOfLink{
		Link: &model.BlockContentLink{
			TargetBlockId: block.GetText().Marks.Marks[0].Param,
			Style:         model.BlockContentLink_Page,
		},
	}
}

func (m *mdConverter) convertTextToBookmark(block *model.Block) {
	if err := uri.ValidateURI(block.GetText().Marks.Marks[0].Param); err != nil {
		return
	}

	block.Content = &model.BlockContentOfBookmark{
		Bookmark: &model.BlockContentBookmark{
			Url: block.GetText().Marks.Marks[0].Param,
		},
	}
}

func (m *mdConverter) convertTextToPageMention(block *model.Block) {
	for _, mark := range block.GetText().Marks.Marks {
		if mark.Type != model.BlockContentTextMark_Link {
			continue
		}
		mark.Type = model.BlockContentTextMark_Mention
	}
}

func (m *mdConverter) convertTextToFile(block *model.Block) {
	// "svg" excluded
	if block.GetText().Marks.Marks[0].Param == "" {
		return
	}

	imageFormats := []string{"jpg", "jpeg", "png", "gif", "webp"}
	videoFormats := []string{"mp4", "m4v"}
	audioFormats := []string{"mp3", "ogg", "wav", "m4a", "flac"}
	pdfFormat := "pdf"

	fileType := model.BlockContentFile_File
	fileExt := filepath.Ext(block.GetText().Marks.Marks[0].Param)
	if fileExt != "" {
		fileExt = fileExt[1:]
		for _, ext := range imageFormats {
			if strings.EqualFold(fileExt, ext) {
				fileType = model.BlockContentFile_Image
				break
			}
		}

		for _, ext := range videoFormats {
			if strings.EqualFold(fileExt, ext) {
				fileType = model.BlockContentFile_Video
				break
			}
		}

		for _, ext := range audioFormats {
			if strings.EqualFold(fileExt, ext) {
				fileType = model.BlockContentFile_Audio
				break
			}
		}

		if strings.EqualFold(fileExt, pdfFormat) {
			fileType = model.BlockContentFile_PDF
		}
	}

	block.Content = &model.BlockContentOfFile{
		File: &model.BlockContentFile{
			Name:  block.GetText().Marks.Marks[0].Param,
			State: model.BlockContentFile_Empty,
			Type:  fileType,
		},
	}
}

func (m *mdConverter) createBlocksFromFile(shortPath string, f io.ReadCloser, files map[string]*FileInfo) error {
	if filepath.Base(shortPath) == shortPath {
		files[shortPath].IsRootFile = true
	}
	if filepath.Ext(shortPath) == ".md" {
		b, err := ioutil.ReadAll(f)
		if err != nil {
			return err
		}
		files[shortPath].ParsedBlocks, _, err = anymark.MarkdownToBlocks(b, filepath.Dir(shortPath), nil)
		if err != nil {
			log.Errorf("failed to read blocks %s: %s", shortPath, err.Error())
		}
		// md file no longer needed
		m.processBlocks(shortPath, files[shortPath], files)
		f.Close()
	} else {
		// need to store file reader, so we can use it to create local file and upload it
		files[shortPath].ReadCloser = f
	}
	return nil
}

func (m *mdConverter) createFile(f *model.BlockContentFile, id string, files map[string]*FileInfo) {
	baseName := filepath.Base(f.Name) + id
	tempDir := m.TempDir()
	newFile := filepath.Join(tempDir, baseName)
	tmpFile, err := os.Create(newFile)
	if err != nil {
		log.Errorf("failed to create file file %s: %s", baseName, err.Error())
		return
	}
	w := bufio.NewWriter(tmpFile)
	shortPath := f.Name
	targetFile, found := files[shortPath]
	if !found {
		log.Errorf("file %s not found", newFile)
		return
	}

	_, err = w.ReadFrom(targetFile.ReadCloser)
	if err != nil {
		log.Errorf("failed to read file %s: %s", shortPath, err.Error())
		return
	}

	if err := w.Flush(); err != nil {
		log.Errorf("failed to flush file %s: %s", shortPath, err.Error())
		return
	}

	targetFile.Close()
	tmpFile.Close()
	f.Name = newFile
}

func (m *mdConverter) processFile(fName string, files map[string]*FileInfo, allErrors ce.ConvertError) {
	shortPath := filepath.Clean(fName)

	f, err := os.Open(fName)
	if err != nil {
		allErrors.Add(shortPath, err)
		log.Errorf("failed to read file %s: %s", shortPath, err.Error())
		return
	}
	files[shortPath] = &FileInfo{}
	files[shortPath].Source = ce.GetSourceDetail(fName, fName)
	if err = m.createBlocksFromFile(shortPath, f, files); err != nil {
		allErrors.Add(shortPath, err)
		log.Errorf("failed to create block from file %s: %s", shortPath, err.Error())
	}
}
