package debug

import (
	"archive/zip"
	"fmt"
	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/change"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
	"github.com/gogo/protobuf/jsonpb"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const CName = "debug"

var logger = logging.Logger("anytype-debug")

func New() Debug {
	return new(debug)
}

type Debug interface {
	app.Component
	DumpTree(blockId, path string, anonymize bool, withSvg bool) (filename string, err error)
	DumpLocalstore(objectIds []string, path string) (filename string, err error)
}

type debug struct {
	core  core.Service
	store objectstore.ObjectStore
}

func (d *debug) Init(a *app.App) (err error) {
	d.core = a.MustComponent(core.CName).(core.Service)
	d.store = a.MustComponent(objectstore.CName).(objectstore.ObjectStore)
	return nil
}

func (d *debug) Name() (name string) {
	return CName
}

func (d *debug) DumpTree(blockId, path string, anonymize bool, withSvg bool) (filename string, err error) {
	// 0 - get first block
	block, err := d.core.GetBlock(blockId)
	if err != nil {
		return
	}

	// 1 - create ZIP file
	// <path>/at.dbg.bafkudtugh626rrqzah3kam4yj4lqbaw4bjayn2rz4ah4n5fpayppbvmq.20220322.121049.23.zip
	builder := &treeBuilder{b: block, s: d.store, anonymized: anonymize}
	zipFilename, err := builder.Build(path)
	if err != nil {
		logger.Fatal("build tree error:", err)
		return "", err
	}

	// if client never asked for SVG generation -> return
	if !withSvg {
		return zipFilename, err
	}

	// 2 - create SVG file near ZIP
	// <path>/at.dbg.bafkudtugh626rrqzah3kam4yj4lqbaw4bjayn2rz4ah4n5fpayppbvmq.20220322.121049.23.svg
	//
	// this will return "graphviz is not supported on the current platform" error if no graphviz!
	// generate a filename just like zip file had
	maxReplacements := 1
	svgFilename := strings.Replace(zipFilename, ".zip", ".svg", maxReplacements)

	err = change.CreateSvg(block, svgFilename)
	if err != nil {
		logger.Fatal("svg build error:", err)
		return "", err
	}

	// return zip filename, but not svgFilename
	return zipFilename, nil
}

func (d *debug) DumpLocalstore(objIds []string, path string) (filename string, err error) {
	if len(objIds) == 0 {
		objIds, err = d.core.ObjectStore().ListIds()
		if err != nil {
			return "", err
		}
	}

	filename = filepath.Join(path, fmt.Sprintf("at.store.dbg.%s.zip", time.Now().Format("20060102.150405.99")))
	f, err := os.Create(filename)
	if err != nil {
		return
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	var wr io.Writer
	m := jsonpb.Marshaler{Indent: " "}

	for _, objId := range objIds {
		doc, err := d.core.ObjectStore().GetWithLinksInfoByID(objId)
		if err != nil {
			var err2 error
			wr, err2 = zw.Create(fmt.Sprintf("%s.txt", objId))
			if err2 != nil {
				return "", err
			}

			wr.Write([]byte(err.Error()))
			continue
		}
		wr, err = zw.Create(fmt.Sprintf("%s.json", objId))
		if err != nil {
			return "", err
		}

		err = m.Marshal(wr, doc)
		if err != nil {
			return "", err
		}
	}
	return filename, nil
}
