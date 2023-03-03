package client

import (
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	db "github.com/cosmos/cosmos-db"
	"github.com/cosmos/iavl"
	"github.com/stretchr/testify/require"
)

func addChangeSet(sub *testing.B, filePath string, tree *iavl.MutableTree) {
	r, err := openChangeSetFile(filePath)
	require.NoError(sub, err)
	IterateChangeSets(r, func(version int64, changeSet *iavl.ChangeSet) (bool, error) {
		for _, pair := range changeSet.Pairs {
			if pair.Delete {
				_, removed, err := tree.Remove(pair.Key)
				require.True(sub, removed)
				require.NoError(sub, err)
			} else {
				_, err := tree.Set(pair.Key, pair.Value)
				require.NoError(sub, err)
			}
		}
		_, _, err := tree.SaveVersion()
		require.NoError(sub, err)
		return true, nil
	})
	h, err := tree.Hash()
	require.NoError(sub, err)
	fmt.Printf("tree size: %d version: %d hash: %v\n", tree.Size(), tree.Version(), h)
}

func runIterationFast(b *testing.B, t *iavl.MutableTree, expectedSize int) {
	isFastCacheEnabled, err := t.IsFastCacheEnabled()
	require.NoError(b, err)
	require.True(b, isFastCacheEnabled) // to ensure fast storage is enabled
	for i := 0; i < b.N; i++ {
		itr, err := t.ImmutableTree.Iterator(nil, nil, false)
		require.NoError(b, err)
		iterate(b, itr, expectedSize)
		require.Nil(b, itr.Close(), ".Close should not error out")
	}
}

func runIterationSlow(b *testing.B, t *iavl.MutableTree, expectedSize int) {
	for i := 0; i < b.N; i++ {
		itr := iavl.NewIterator(nil, nil, false, t.ImmutableTree) // create slow iterator directly
		iterate(b, itr, expectedSize)
		require.Nil(b, itr.Close(), ".Close should not error out")
	}
}

func iterate(b *testing.B, itr db.Iterator, expectedSize int) {
	b.StartTimer()
	keyValuePairs := make([][][]byte, 0, expectedSize)
	for i := 0; i < expectedSize && itr.Valid(); i++ {
		itr.Next()
		keyValuePairs = append(keyValuePairs, [][]byte{itr.Key(), itr.Value()})
	}
	b.StopTimer()
	if g, w := len(keyValuePairs), expectedSize; g != w {
		b.Errorf("iteration count mismatch: got=%d, want=%d", g, w)
	} else if testing.Verbose() {
		b.Logf("completed %d iterations", len(keyValuePairs))
	}
}

func randBytes(length int) []byte {
	key := make([]byte, length)
	// math.rand.Read always returns err=nil
	// we do not need cryptographic randomness for this test:
	rand.Read(key)
	return key
}

// queries random keys against live state. Keys are almost certainly not in the tree.
func runQueriesFast(b *testing.B, t *iavl.MutableTree, keyLen int) {
	isFastCacheEnabled, err := t.IsFastCacheEnabled()
	require.NoError(b, err)
	require.True(b, isFastCacheEnabled)
	for i := 0; i < b.N; i++ {
		q := randBytes(keyLen)
		_, err := t.Get(q)
		require.NoError(b, err)
	}
}

func runQueriesSlow(b *testing.B, t *iavl.MutableTree, keyLen int) {
	b.StopTimer()
	// Save version to get an old immutable tree to query against,
	// Fast storage is not enabled on old tree versions, allowing us to bench the desired behavior.
	_, version, err := t.SaveVersion()
	require.NoError(b, err)

	itree, err := t.GetImmutable(version - 1)
	require.NoError(b, err)
	isFastCacheEnabled, err := itree.IsFastCacheEnabled() // to ensure fast storage is enabled
	require.NoError(b, err)
	require.False(b, isFastCacheEnabled) // to ensure fast storage is not enabled

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		q := randBytes(keyLen)
		_, _, err := itree.GetWithIndex(q)
		require.NoError(b, err)
	}
}

func BenchmarkChangeSet(b *testing.B) {
	basePath := "../networks"
	entries, err := os.ReadDir(basePath)
	require.NoError(b, err)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirName := e.Name() // cronos-testnet3-7241674
		res := strings.Split(dirName, "-")
		lastVersion, err := strconv.Atoi(res[len(res)-1])
		require.NoError(b, err)
		modules, err := os.ReadDir(basePath + "/" + dirName)
		require.NoError(b, err)
		for _, m := range modules {
			if !m.IsDir() {
				continue
			}
			moduleName := m.Name() // acc
			prefix := fmt.Sprintf("%s-%s", dirName, moduleName)
			// prepare a dir for the db and cleanup afterwards
			dbDirName := fmt.Sprintf("./%s-%s-db", res[0], moduleName)
			d, err := db.NewDB("test", db.GoLevelDBBackend, dbDirName)
			require.NoError(b, err)

			tree, err := iavl.NewMutableTreeWithOpts(d, 50000, nil, false)
			require.NoError(b, err)

			b.Run(prefix+"-init", func(sub *testing.B) {
				sub.ReportAllocs()
				setFiles, err := os.ReadDir(basePath + "/" + dirName + "/" + moduleName)
				require.NoError(sub, err)
				for _, f := range setFiles {
					addChangeSet(sub, basePath+"/"+dirName+"/"+moduleName+"/"+f.Name(), tree)
					break // only one file
				}
				fmt.Printf("latest version: %d, expected: %d\n", tree.Version(), int64(lastVersion))
			})

			b.Run(prefix+"-query-no-in-tree-guarantee-fast", func(sub *testing.B) {
				sub.ReportAllocs()
				runQueriesFast(sub, tree, 16)
			})
			b.Run(prefix+"-query-no-in-tree-guarantee-slow", func(sub *testing.B) {
				sub.ReportAllocs()
				runQueriesSlow(sub, tree, 16)
			})

			b.Run(prefix+"-iteration-fast", func(sub *testing.B) {
				sub.ReportAllocs()
				runIterationFast(sub, tree, int(tree.Size()))
			})
			b.Run(prefix+"-iteration-slow", func(sub *testing.B) {
				sub.ReportAllocs()
				runIterationSlow(sub, tree, int(tree.Size()))
			})

			err = os.RemoveAll(dbDirName)
			if err != nil {
				b.Errorf("%+v\n", err)
			}
		}
	}
}
