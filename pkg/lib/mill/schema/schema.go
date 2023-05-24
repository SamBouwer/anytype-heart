package schema

import (
	"fmt"

	ipld "github.com/ipfs/go-ipld-format"

	"github.com/anyproto/anytype-heart/pkg/lib/pb/storage"
)

// ErrEmptySchema indicates a schema is empty
var ErrEmptySchema = fmt.Errorf("schema does not create any files")

// ErrLinkOrderNotSolvable
var ErrLinkOrderNotSolvable = fmt.Errorf("link order is not solvable")

// FileTag indicates the link should "use" the input file as source
const FileTag = ":file"

// SingleFileTag is a magic key indicating that a directory is actually a single file
const SingleFileTag = ":single"

// LinkByName finds a link w/ one of the given names in the provided list
func LinkByName(links []*ipld.Link, names []string) *ipld.Link {
	for _, l := range links {
		for _, n := range names {
			if l.Name == n {
				return l
			}
		}
	}
	return nil
}

// Steps returns link steps in the order they should be processed
func Steps(links map[string]*storage.Link) ([]storage.Step, error) {
	var steps []storage.Step
	run := links
	i := 0
	for {
		if i > len(links) {
			return nil, ErrLinkOrderNotSolvable
		}
		next := orderLinks(run, &steps)
		if len(next) == 0 {
			break
		}
		run = next
		i++
	}
	return steps, nil
}

// orderLinks attempts to place all links in steps, returning any unused
// whose source is not yet in steps
func orderLinks(links map[string]*storage.Link, steps *[]storage.Step) map[string]*storage.Link {
	unused := make(map[string]*storage.Link)
	for name, link := range links {
		if link.Use == FileTag {
			*steps = append([]storage.Step{{Name: name, Link: link}}, *steps...)
		} else {
			useAt := -1
			for i, s := range *steps {
				if link.Use == s.Name {
					useAt = i
					break
				}
			}
			if useAt >= 0 {
				*steps = append(*steps, storage.Step{Name: name, Link: link})
			} else {
				unused[name] = link
			}
		}
	}
	return unused
}
