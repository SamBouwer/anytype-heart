package state

import (
	"math/rand"
	"testing"

	"github.com/globalsign/mgo/bson"
	"github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anyproto/anytype-heart/core/block/simple"
	"github.com/anyproto/anytype-heart/core/block/simple/base"
	"github.com/anyproto/anytype-heart/core/block/simple/text"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/util/pbtypes"
)

func TestState_Add(t *testing.T) {
	s := NewDoc("1", nil).NewState()
	assert.Nil(t, s.Get("1"))
	assert.True(t, s.Add(base.NewBase(&model.Block{
		Id: "1",
	})))
	assert.NotNil(t, s.Get("1"))
	assert.False(t, s.Add(base.NewBase(&model.Block{
		Id: "1",
	})))
}

func TestState_AddRemoveAdd(t *testing.T) {
	s := NewDoc("1", nil).NewState()
	assert.Nil(t, s.Get("1"))
	assert.True(t, s.Add(base.NewBase(&model.Block{
		Id: "1",
		Content: &model.BlockContentOfDataview{Dataview: &model.BlockContentDataview{Views: []*model.BlockContentDataviewView{{
			Id:   "v1",
			Name: "v1",
		}}}},
	})))
	assert.NotNil(t, s.Get("1"))
	s.Unlink("1")
	s.Set(base.NewBase(&model.Block{
		Id: "1",
		Content: &model.BlockContentOfDataview{Dataview: &model.BlockContentDataview{Views: []*model.BlockContentDataviewView{{
			Id:   "v1",
			Name: "v1",
		}}}},
	}))
	assert.False(t, s.Add(base.NewBase(&model.Block{
		Id: "1",
	})))
}

func TestState_Get(t *testing.T) {
	s := NewDoc("1", map[string]simple.Block{
		"1": base.NewBase(&model.Block{Id: "1"}),
	}).NewState()
	assert.NotNil(t, s.Get("1"))
	assert.NotNil(t, s.NewState().Get("1"))
}

func TestState_Pick(t *testing.T) {
	s := NewDoc("1", map[string]simple.Block{
		"1": base.NewBase(&model.Block{Id: "1"}),
	}).NewState()
	assert.NotNil(t, s.Pick("1"))
	assert.NotNil(t, s.NewState().Pick("1"))
}

func TestState_Unlink(t *testing.T) {
	s := NewDoc("1", map[string]simple.Block{
		"1": base.NewBase(&model.Block{Id: "1", ChildrenIds: []string{"2"}}),
		"2": base.NewBase(&model.Block{Id: "2"}),
	}).NewState()
	assert.True(t, s.Unlink("2"))
	assert.Len(t, s.Pick("1").Model().ChildrenIds, 0)
	assert.False(t, s.Unlink("2"))
}

func TestState_GetParentOf(t *testing.T) {
	t.Run("generic", func(t *testing.T) {
		s := NewDoc("1", map[string]simple.Block{
			"1": base.NewBase(&model.Block{Id: "1", ChildrenIds: []string{"2"}}),
			"2": base.NewBase(&model.Block{Id: "2"}),
		}).NewState()
		assert.Equal(t, "1", s.GetParentOf("2").Model().Id)
	})
	t.Run("direct", func(t *testing.T) {
		s := NewDoc("1", map[string]simple.Block{
			"1": base.NewBase(&model.Block{Id: "1", ChildrenIds: []string{"2"}}),
			"2": base.NewBase(&model.Block{Id: "2"}),
		}).(*State)
		assert.Equal(t, "1", s.GetParentOf("2").Model().Id)
	})
}

func TestApplyState(t *testing.T) {
	t.Run("intermediate apply", func(t *testing.T) {
		d := NewDoc("1", map[string]simple.Block{
			"1": base.NewBase(&model.Block{Id: "1", ChildrenIds: []string{"2"}}),
			"2": base.NewBase(&model.Block{Id: "2"}),
		})
		s := d.NewState()
		s.Add(simple.New(&model.Block{Id: "3"}))
		s.InsertTo("2", model.Block_Bottom, "3")
		s.changeId = "1"

		s = s.NewState()
		s.Add(simple.New(&model.Block{Id: "4"}))
		s.InsertTo("3", model.Block_Bottom, "4")
		s.changeId = "2"

		s = s.NewState()
		s.Unlink("3")
		s.changeId = "3"

		s = s.NewState()
		s.Add(simple.New(&model.Block{Id: "5"}))
		s.InsertTo("4", model.Block_Bottom, "5")
		s.changeId = "4"

		msgs, hist, err := ApplyState(s, true)
		require.NoError(t, err)
		assert.Len(t, hist.Add, 2)
		assert.Len(t, hist.Change, 1)
		assert.Len(t, hist.Remove, 0)
		require.Len(t, msgs, 2)
	})
	t.Run("details handle", func(t *testing.T) {
		t.Run("text", func(t *testing.T) {
			d := NewDoc("1", map[string]simple.Block{
				"1": base.NewBase(&model.Block{Id: "1", ChildrenIds: []string{"2"}}),
				"2": text.NewDetails(&model.Block{
					Id: "2",
					Content: &model.BlockContentOfText{
						Text: &model.BlockContentText{},
					},
					Fields: &types.Struct{
						Fields: map[string]*types.Value{
							text.DetailsKeyFieldName: pbtypes.String("name"),
						},
					},
				}, text.DetailsKeys{
					Text: "name",
				}),
			})
			d.BlocksInit(d.(simple.DetailsService))
			s := d.NewState()
			s.SetDetails(&types.Struct{
				Fields: map[string]*types.Value{
					"name": pbtypes.String("new name"),
				},
			})
			msgs, _, err := ApplyState(s, true)
			require.NoError(t, err)
			assert.Len(t, msgs, 2)
		})
		t.Run("checked", func(t *testing.T) {
			d := NewDoc("1", map[string]simple.Block{
				"1": base.NewBase(&model.Block{Id: "1", ChildrenIds: []string{"2"}}),
				"2": text.NewDetails(&model.Block{
					Id: "2",
					Content: &model.BlockContentOfText{
						Text: &model.BlockContentText{},
					},
					Fields: &types.Struct{
						Fields: map[string]*types.Value{
							text.DetailsKeyFieldName: pbtypes.StringList([]string{"", "done"}),
						},
					},
				}, text.DetailsKeys{
					Checked: "done",
				}),
			})
			d.(*State).SetDetail("done", pbtypes.Bool(true))
			d.BlocksInit(d.(simple.DetailsService))
			s := d.NewState()
			s.SetDetails(&types.Struct{
				Fields: map[string]*types.Value{
					"done": pbtypes.Bool(false),
				},
			})
			msgs, _, err := ApplyState(s, true)
			require.NoError(t, err)
			assert.Len(t, msgs, 2)
		})
		t.Run("setChecked", func(t *testing.T) {
			d := NewDoc("1", map[string]simple.Block{
				"1": base.NewBase(&model.Block{Id: "1", ChildrenIds: []string{"2"}}),
				"2": text.NewDetails(&model.Block{
					Id: "2",
					Content: &model.BlockContentOfText{
						Text: &model.BlockContentText{},
					},
					Fields: &types.Struct{
						Fields: map[string]*types.Value{
							text.DetailsKeyFieldName: pbtypes.StringList([]string{"", "done"}),
						},
					},
				}, text.DetailsKeys{
					Checked: "done",
				}),
			})
			d.(*State).SetDetail("done", pbtypes.Bool(true))
			d.BlocksInit(d.(simple.DetailsService))
			s := d.NewState()
			s.Get("2").(text.Block).SetChecked(false)
			msgs, _, err := ApplyState(s, true)
			require.NoError(t, err)
			assert.Len(t, msgs, 2)
		})
	})

}

func TestState_IsChild(t *testing.T) {
	s := NewDoc("root", map[string]simple.Block{
		"root": base.NewBase(&model.Block{Id: "root", ChildrenIds: []string{"2"}}),
		"2":    base.NewBase(&model.Block{Id: "2", ChildrenIds: []string{"3"}}),
		"3":    base.NewBase(&model.Block{Id: "3"}),
	}).NewState()
	assert.True(t, s.IsChild("2", "3"))
	assert.True(t, s.IsChild("root", "3"))
	assert.True(t, s.IsChild("root", "2"))
	assert.False(t, s.IsChild("root", "root"))
	assert.False(t, s.IsChild("3", "2"))
}

func TestState_HasParent(t *testing.T) {
	s := NewDoc("root", map[string]simple.Block{
		"root": base.NewBase(&model.Block{Id: "root", ChildrenIds: []string{"1", "2"}}),
		"1":    base.NewBase(&model.Block{Id: "1"}),
		"2":    base.NewBase(&model.Block{Id: "2", ChildrenIds: []string{"3"}}),
		"3":    base.NewBase(&model.Block{Id: "3"}),
	}).NewState()
	t.Run("not parent", func(t *testing.T) {
		assert.False(t, s.HasParent("1", "2"))
		assert.False(t, s.HasParent("1", ""))
	})
	t.Run("parent", func(t *testing.T) {
		assert.True(t, s.HasParent("3", "2"))
		assert.True(t, s.HasParent("3", "root"))
		assert.True(t, s.HasParent("2", "root"))
	})
}

func BenchmarkState_Iterate(b *testing.B) {
	newBlock := func(id string, childrenIds ...string) simple.Block {
		return simple.New(&model.Block{Id: id, ChildrenIds: childrenIds})
	}

	s := NewDoc("root", nil).NewState()
	root := newBlock("root")
	s.Add(root)
	for i := 0; i < 100; i++ {
		ch1Id := bson.NewObjectId().Hex()
		root.Model().ChildrenIds = append(root.Model().ChildrenIds, ch1Id)
		ch1 := newBlock(ch1Id)
		s.Add(ch1)
		for j := 0; j < 10; j++ {
			ch2Id := bson.NewObjectId().Hex()
			ch2 := newBlock(ch2Id)
			ch1.Model().ChildrenIds = append(ch1.Model().ChildrenIds, ch2Id)
			s.Add(ch2)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		s.Iterate(func(b simple.Block) (isContinue bool) {
			return true
		})
	}
}

func TestState_IsEmpty(t *testing.T) {
	t.Run("without title block", func(t *testing.T) {
		s := NewDoc("root", map[string]simple.Block{
			"root": simple.New(&model.Block{
				Id:          "root",
				ChildrenIds: []string{"header", "emptyText"},
			}),
			"header": simple.New(&model.Block{Id: "header"}),
			"emptyText": simple.New(&model.Block{Id: "emptyText",
				Content: &model.BlockContentOfText{
					Text: &model.BlockContentText{Marks: &model.BlockContentTextMarks{}},
				}}),
		}).(*State)
		assert.True(t, s.IsEmpty(true))
		s.Pick("emptyText").Model().GetText().Text = "1"
		assert.False(t, s.IsEmpty(true))
	})

	t.Run("with title block", func(t *testing.T) {
		s := NewDoc("root", map[string]simple.Block{
			"root": simple.New(&model.Block{
				Id:          "root",
				ChildrenIds: []string{"header"},
			}),
			"header": simple.New(&model.Block{Id: "header", ChildrenIds: []string{"title"}}),
			"title": simple.New(&model.Block{Id: "title",
				Content: &model.BlockContentOfText{
					Text: &model.BlockContentText{Marks: &model.BlockContentTextMarks{}},
				}}),
		}).(*State)

		assert.True(t, s.IsEmpty(true))
		assert.True(t, s.IsEmpty(false))

		s.Pick("title").Model().GetText().Text = "1"
		assert.False(t, s.IsEmpty(true))
		assert.True(t, s.IsEmpty(false))
	})

	t.Run("with title block and empty block", func(t *testing.T) {
		s := NewDoc("root", map[string]simple.Block{
			"root": simple.New(&model.Block{
				Id:          "root",
				ChildrenIds: []string{"header", "emptyText"},
			}),
			"header": simple.New(&model.Block{Id: "header", ChildrenIds: []string{"title"}}),
			"title": simple.New(&model.Block{Id: "title",
				Content: &model.BlockContentOfText{
					Text: &model.BlockContentText{Marks: &model.BlockContentTextMarks{}},
				}}),
			"emptyText": simple.New(&model.Block{Id: "emptyText",
				Content: &model.BlockContentOfText{
					Text: &model.BlockContentText{Marks: &model.BlockContentTextMarks{}},
				}}),
		}).(*State)

		assert.False(t, s.IsEmpty(true))
		assert.False(t, s.IsEmpty(false))
	})
}

func TestState_Descendants(t *testing.T) {
	for _, tc := range []struct {
		name   string
		blocks []*model.Block
		rootId string
		want   []string
	}{
		{
			name: "root is absent",
			blocks: []*model.Block{
				{Id: "test"},
			},
			rootId: "foo",
			want:   []string{},
		},
		{
			name: "root without descendants",
			blocks: []*model.Block{
				{Id: "test"},
			},
			rootId: "test",
			want:   []string{},
		},
		{
			name: "root with one level of descendants",
			blocks: []*model.Block{
				{Id: "test", ChildrenIds: []string{"1", "2"}},
				{Id: "1"},
				{Id: "2"},
			},
			rootId: "test",
			want:   []string{"1", "2"},
		},
		{
			name: "root with one level of descendants and some blocks are nil",
			blocks: []*model.Block{
				{Id: "test", ChildrenIds: []string{"1", "2"}},
				{Id: "1"},
			},
			rootId: "test",
			want:   []string{"1"},
		},
		{
			name: "root with multiple level of descendants",
			blocks: []*model.Block{
				{Id: "test", ChildrenIds: []string{"1", "2"}},
				{Id: "1", ChildrenIds: []string{"1.1", "1.2"}},
				{Id: "1.1"},
				{Id: "1.2", ChildrenIds: []string{"1.2.1", "1.2.2"}},
				{Id: "1.2.1"},
				{Id: "1.2.2"},
				{Id: "2", ChildrenIds: []string{"2.1"}},
				{Id: "2.1"},
			},
			rootId: "test",
			want:   []string{"1", "2", "1.1", "1.2", "1.2.1", "1.2.2", "2.1"},
		},

		{
			name: "complex tree and request for descendants of middle node",
			blocks: []*model.Block{
				{Id: "test", ChildrenIds: []string{"1", "2"}},
				{Id: "1", ChildrenIds: []string{"1.1", "1.2"}},
				{Id: "1.1"},
				{Id: "1.2", ChildrenIds: []string{"1.2.1", "1.2.2"}},
				{Id: "1.2.1"},
				{Id: "1.2.2"},
				{Id: "2", ChildrenIds: []string{"2.1"}},
				{Id: "2.1"},
			},
			rootId: "1.2",
			want:   []string{"1.2.1", "1.2.2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewDoc("root", nil).NewState()
			for _, b := range tc.blocks {
				s.Add(simple.New(b))
			}

			got := s.Descendants(tc.rootId)

			gotIds := make([]string, 0, len(got))
			for _, b := range got {
				b2 := s.Pick(b.Model().Id)
				require.NotNil(t, b2)
				assert.Equal(t, b2, b)

				gotIds = append(gotIds, b.Model().Id)
			}

			assert.ElementsMatch(t, tc.want, gotIds)
		})
	}
}

func TestState_SelectRoots(t *testing.T) {
	t.Run("simple state", func(t *testing.T) {
		s := NewDoc("root", nil).NewState()
		s.Add(mkBlock("root", "1", "2", "3"))
		s.Add(mkBlock("1"))
		s.Add(mkBlock("2", "2.1"))
		s.Add(mkBlock("3"))

		assert.Equal(t, []string{"root"}, s.SelectRoots([]string{"root", "2", "3"}))
		assert.Equal(t, []string{"root"}, s.SelectRoots([]string{"3", "root", "2"}))
		assert.Equal(t, []string{"1", "2"}, s.SelectRoots([]string{"1", "2", "2.1"}))
		assert.Equal(t, []string{}, s.SelectRoots([]string{"4"}))
	})

	t.Run("with complex state", func(t *testing.T) {
		s := mkComplexState()

		assert.Equal(t, []string{"root"}, s.SelectRoots([]string{"root", "1.3.4"}))
		assert.Equal(t, []string{"1.3.4"}, s.SelectRoots([]string{"1.3.4"}))
		assert.Equal(t, []string{"1.1", "1.2", "1.3"}, s.SelectRoots([]string{"1.1", "1.2", "1.3"}))
		assert.Equal(t, []string{"1.1", "1.2", "1.3"}, s.SelectRoots([]string{"1.1", "1.2", "1.3"}))

		t.Run("chaotic args", func(t *testing.T) {
			var allIds []string
			for _, b := range s.Blocks() {
				allIds = append(allIds, b.Id)
			}
			for i := 0; i < len(allIds); i++ {
				rand.Shuffle(len(allIds), func(i, j int) { allIds[i], allIds[j] = allIds[j], allIds[i] })
				assert.Equal(t, []string{"root"}, s.SelectRoots(allIds))
			}
		})
	})
}

func mkBlock(id string, children ...string) simple.Block {
	return simple.New(&model.Block{Id: id, ChildrenIds: children})
}

func mkComplexState() *State {
	s := NewDoc("root", nil).NewState()
	for _, b := range []simple.Block{
		mkBlock("root", "1", "2", "3"),
		mkBlock("1", "1.1", "1.2", "1.3"),
		mkBlock("1.1"),
		mkBlock("1.2"),
		mkBlock("1.3", "1.3.1", "1.3.2", "1.3.3", "1.3.4"),
		mkBlock("1.3.1"),
		mkBlock("1.3.2"),
		mkBlock("1.3.3"),
		mkBlock("1.3.4"),
		mkBlock("2"),
		mkBlock("3"),
	} {
		s.Add(b)
	}
	return s
}

func BenchmarkState_SelectRoots(b *testing.B) {
	s := mkComplexState()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = s.SelectRoots([]string{"3", "root", "2", "1.3.1", "1.2", "1.3", "1.1"})
	}
}

func TestState_GetChangedStoreKeys(t *testing.T) {
	p := NewDoc("test", nil).(*State)
	p.SetInStore([]string{"one", "two", "v1"}, pbtypes.String("val1"))
	p.SetInStore([]string{"one", "two", "v2"}, pbtypes.String("val2"))
	p.SetInStore([]string{"one", "two", "v3"}, pbtypes.String("val3"))
	p.SetInStore([]string{"other"}, pbtypes.String("val42"))
	changed := p.GetChangedStoreKeys()

	s := p.NewState()
	s.SetInStore([]string{"one", "two", "v2"}, pbtypes.String("val2ch"))
	s.SetInStore([]string{"other"}, pbtypes.String("changed"))
	s.RemoveFromStore([]string{"one", "two", "v3"})

	changed = s.GetChangedStoreKeys()
	assert.Len(t, changed, 3)
	assert.Contains(t, changed, []string{"one", "two", "v2"})
	assert.Contains(t, changed, []string{"one", "two"})
	assert.Contains(t, changed, []string{"other"})

	changed = s.GetChangedStoreKeys("one")
	assert.Len(t, changed, 2)
	changed = s.GetChangedStoreKeys("one", "two", "v2")
	assert.Len(t, changed, 1)
}
