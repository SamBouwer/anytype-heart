package converter

import (
	"io"

	"github.com/anyproto/anytype-heart/core/block/process"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
)

// Functions to create in-tree and plugin converters
var converterCreators []ConverterCreator

// Function to register converter
type ConverterCreator = func(s core.Service) Converter

// RegisterFunc add converter creation function to converterCreators
func RegisterFunc(c ConverterCreator) {
	converterCreators = append(converterCreators, c)
}

// Converter incapsulate logic with transforming some data to smart blocks
type Converter interface {
	GetSnapshots(req *pb.RpcObjectImportRequest, progress *process.Progress) (*Response, ConvertError)
	Name() string
}

// ImageGetter returns image for given converter in frontend
type ImageGetter interface {
	GetImage() ([]byte, int64, int64, error)
}

// IOReader combine name of the file and it's io reader
type IOReader struct {
	Name   string
	Reader io.ReadCloser
}
type Snapshot struct {
	Id       string
	FileName string
	Snapshot *model.SmartBlockSnapshotBase
}

// during GetSnapshots step in converter and create them in RelationCreator
type Relation struct {
	BlockID string // if relations is used as a block
	*model.Relation
}

// Response expected response of each converter, incapsulate blocks snapshots and converting errors
type Response struct {
	Snapshots []*Snapshot
	Relations map[string][]*Relation // object id to its relations
}

func GetConverters() []func(s core.Service) Converter {
	return converterCreators
}
