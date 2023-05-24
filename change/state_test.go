package change

import (
	"bytes"
	"context"
	"encoding/gob"
	"io/ioutil"
	"testing"
	"time"

	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/stretchr/testify/require"
)

func BenchmarkOpenDoc(b *testing.B) {
	data, err := ioutil.ReadFile("./testdata/bench_changes_short_ids.pb")
	require.NoError(b, err)
	dec := gob.NewDecoder(bytes.NewReader(data))
	var changeSet map[string][]byte
	require.NoError(b, dec.Decode(&changeSet))

	sb := NewTestSmartBlock()
	sb.changes = make(map[string]*core.SmartblockRecordEnvelope)
	for k, v := range changeSet {
		sb.changes[k] = &core.SmartblockRecordEnvelope{
			SmartblockRecord: core.SmartblockRecord{Payload: v},
		}
	}
	b.Log("changes:", len(sb.changes))
	sb.logs = append(sb.logs, core.SmartblockLog{
		ID:   "one",
		Head: "bafyreidqwqpaiu6gvdstpkekj3fnkxpqisdkdzrjg3ykjpqb4ciym3w4ya",
	})

	st := time.Now()
	tree, _, e := BuildTree(context.Background(), sb)
	require.NoError(b, e)
	b.Log("build tree:", time.Since(st))
	b.Log(tree.Len())

	st = time.Now()
	root := tree.Root()
	doc := state.NewDocFromSnapshot("bafybapt3aap3tmkbs7mkj5jao3vhjblijkiwqq37wxlylx5nn7cqokgk", root.GetSnapshot()).(*state.State)
	doc.SetChangeId(root.Id)
	_, _, err = BuildStateSimpleCRDT(doc, tree)
	require.NoError(b, err)
	b.Log("build state:", time.Since(st))

	b.Run("build tree", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, err := BuildTree(context.Background(), sb)
			require.NoError(b, err)
		}
	})
	b.Run("build state", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			doc := state.NewDocFromSnapshot("bafybapt3aap3tmkbs7mkj5jao3vhjblijkiwqq37wxlylx5nn7cqokgk", root.GetSnapshot()).(*state.State)
			doc.SetChangeId(root.Id)
			_, _, err := BuildStateSimpleCRDT(doc, tree)
			require.NoError(b, err)
		}
	})
}
