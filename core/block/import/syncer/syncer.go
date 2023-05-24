package syncer

import "github.com/anyproto/anytype-heart/core/block/simple"

type Factory struct {
	fs Syncer
	bs Syncer
	is Syncer
}

func New(fs *FileSyncer, bs *BookmarkSyncer, is *IconSyncer) *Factory {
	return &Factory{fs: fs, bs: bs, is: is}
}

func (f *Factory) GetSyncer(b simple.Block) Syncer {
	if bm := b.Model().GetBookmark(); bm != nil {
		return f.bs
	}
	if file := b.Model().GetFile(); file != nil {
		return f.fs
	}
	if b.Model().GetText() != nil && b.Model().GetText().GetIconImage() != "" {
		return f.is
	}
	return nil
}
