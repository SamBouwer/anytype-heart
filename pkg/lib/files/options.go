package files

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/anyproto/anytype-heart/pkg/lib/pb/storage"
	"github.com/h2non/filetype"
	ipfspath "github.com/ipfs/go-path"
)

type AddOption func(*AddOptions)
type AddOptions struct {
	Reader    io.ReadSeeker
	Use       string
	Media     string
	Name      string
	Plaintext bool
}

func WithReader(r io.ReadSeeker) AddOption {
	return func(args *AddOptions) {
		args.Reader = r
	}
}

func WithName(name string) AddOption {
	return func(args *AddOptions) {
		args.Name = name
	}
}

func (s *Service) NormalizeOptions(ctx context.Context, opts *AddOptions) error {
	if opts.Use != "" {
		ref, err := ipfspath.ParsePath(opts.Use)
		if err != nil {
			return err
		}
		parts := strings.Split(ref.String(), "/")
		hash := parts[len(parts)-1]
		var file *storage.FileInfo

		opts.Reader, file, err = s.fileContent(ctx, hash)
		if err != nil {
			/*if err == localstore.ErrNotFound{
				// just cat the data from ipfs
				b, err := ipfsutil.DataAtCid(s.ipfs, ref.String())
				if err != nil {
					return nil, err
				}
				reader = bytes.NewReader(b)
				conf.Use = ref.String()
			} else {*/
			return err
		} else {
			opts.Use = file.Checksum
		}
	}

	if opts.Media == "" {
		data, err := ioutil.ReadAll(io.LimitReader(opts.Reader, 512))
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to get first 512 bytes to detect content-type: %s", err)
		}

		_, err = opts.Reader.Seek(0, io.SeekStart)
		if err != nil {
			return fmt.Errorf("failed to seek underlying reader: %w", err)
		}

		t, err := filetype.Match(data)
		if err != nil {
			log.Warnf("filetype failed to match for %s: %s", opts.Name, err.Error())
			opts.Media = http.DetectContentType(data)
		} else {
			opts.Media = t.MIME.Value
		}
	}

	return nil
}
