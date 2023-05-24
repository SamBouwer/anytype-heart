package latex

import (
	"fmt"

	"github.com/anyproto/anytype-heart/core/block/simple"
	"github.com/anyproto/anytype-heart/core/block/simple/base"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/util/pbtypes"
)

func init() {
	simple.RegisterCreator(NewLatex)
}

func NewLatex(m *model.Block) simple.Block {
	if latex := m.GetLatex(); latex != nil {
		return &Latex{
			Base:    base.NewBase(m).(*base.Base),
			content: latex,
		}
	}
	return nil
}

type Block interface {
	simple.Block
	SetText(text string)
	ApplyEvent(e *pb.EventBlockSetLatex) error
}

var _ Block = (*Latex)(nil)

type Latex struct {
	*base.Base
	content *model.BlockContentLatex
}

func (l *Latex) Copy() simple.Block {
	copy := pbtypes.CopyBlock(l.Model())
	return &Latex{
		Base:    base.NewBase(copy).(*base.Base),
		content: copy.GetLatex(),
	}
}

// Validate TODO: add validation rules
func (l *Latex) Validate() error {
	return nil
}

func (l *Latex) Diff(b simple.Block) (msgs []simple.EventMessage, err error) {
	latex, ok := b.(*Latex)
	if !ok {
		return nil, fmt.Errorf("can't make diff with different block type")
	}
	if msgs, err = l.Base.Diff(latex); err != nil {
		return
	}
	changes := &pb.EventBlockSetLatex{
		Id: latex.Id,
	}
	hasChanges := false

	if l.content.Text != latex.content.Text {
		hasChanges = true
		changes.Text = &pb.EventBlockSetLatexText{Value: latex.content.Text}
	}

	if hasChanges {
		msgs = append(msgs, simple.EventMessage{Msg: &pb.EventMessage{Value: &pb.EventMessageValueOfBlockSetLatex{BlockSetLatex: changes}}})
	}
	return
}

func (r *Latex) SetText(text string) {
	r.content.Text = text
}

func (l *Latex) ApplyEvent(e *pb.EventBlockSetLatex) error {
	if e.Text != nil {
		l.content.Text = e.Text.GetValue()
	}
	return nil
}
