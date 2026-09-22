package source

import (
	"bytes"
	"io"
	"os"
)

// ReadNew returns the complete lines appended to path since offset and the
// offset just past the last newline. A partial trailing line is left for the
// next call. If the file shrank below offset (rotated/rewritten) it restarts
// from 0.
func ReadNew(path string, offset int64) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if offset > st.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}
	cut := bytes.LastIndexByte(data, '\n')
	if cut < 0 {
		return nil, offset, nil
	}
	return data[:cut+1], offset + int64(cut+1), nil
}
